/*
@author: sk
@date: 2025/5/4
*/
package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"math"
	"os"
	"strconv"
)

type SofItem struct {
	Type  uint8 // 1 Y  2 Cb  3 Cr
	W, H  uint8 // 垂直，水平采样率
	DqtID uint8 // 量化表
}

type Sof struct {
	Accuracy   uint8 // 固定 8
	Width      uint16
	Height     uint16
	ColorCount uint8 // 固定为 3
	Items      map[uint8]*SofItem
}

type SosItem struct {
	Type    uint8 // 1 Y  2 Cb  3 Cr
	QhtDcID uint8 // 赫夫曼表 dc id
	QhtAcID uint8 // 赫夫曼表 ac id
}

type Sos struct {
	ColorCount uint8 // 固定为 3
	Items      map[uint8]*SosItem
	Unused     []uint8 // 固定为  0x003F00  3 byte
}

// https://github.com/MROS/jpeg_tutorial

func main() {
	reader, err := os.Open("images.jpeg")
	HandleErr(err)
	dqts := make(map[uint8][]uint16)         // id -> 量化表
	dhts := make(map[uint8]map[string]uint8) // id -> 编码表
	var sof *Sof
	var sos *Sos
	for {
		if ReadU8(reader) != 0xFF {
			continue
		}
		cmd := ReadU8(reader)
		if cmd == 0xD8 {
			fmt.Println("SOI") // 文档开始
		} else if cmd == 0xD9 {
			fmt.Println("EOI") // 文档结束
			break
		} else if cmd == 0xDB {
			fmt.Println("DQT") // 量化表
			size := ReadU16(reader)
			data := ReadByteWithSkip(reader, int(size-2)) // 还要移除开始的长度
			HandleDQT(dqts, data)
		} else if cmd == 0xC4 {
			fmt.Println("DHT") // 赫夫曼表
			size := ReadU16(reader)
			data := ReadByteWithSkip(reader, int(size-2)) // 还要移除开始的长度
			HandleDHT(dhts, data)
			//PrintString(dhts)
		} else if cmd == 0xC0 {
			fmt.Println("SOF0")
			size := ReadU16(reader)
			data := ReadByteWithSkip(reader, int(size-2)) // 还要移除开始的长度
			sof = ParseSOF(data)
			//PrintString(sof)
		} else if cmd == 0xDA {
			//PrintString(dhts)
			fmt.Println("SOS")
			size := ReadU16(reader)
			data := ReadByteWithSkip(reader, int(size-2)) // 还要移除开始的长度
			sos = ParseSOS(data)
			//PrintString(sos)
			ReadMcus(reader, sos, sof, dqts, dhts)
		} else {
			//fmt.Println("不支持")
		}
	}
	//fmt.Println(sos)
	//fmt.Println(sof)
	//fmt.Println(dqts)
	//fmt.Println(dhts)
}

func ReadMcus(reader io.Reader, sos *Sos, sof *Sof, dqts map[uint8][]uint16, dhts map[uint8]map[string]uint8) {
	maxW := 0
	maxH := 0
	for _, item := range sof.Items { // 找到最大采样计算 mcu 的大小 实际就是取  Y 的采样 * 8   每个块 8*8 Y 是全采样
		maxW = max(maxW, int(item.W*8))
		maxH = max(maxH, int(item.H*8))
	}
	xCount := (int(sof.Width) + maxW - 1) / maxW  // 取恰好够个块
	yCount := (int(sof.Height) + maxH - 1) / maxH // 取恰好够个块
	// 从这里开始 reader 会按 bit 读  创建的图片按原始大小来，填充分块数据时可能越界内部会自动校验
	img := image.NewRGBA(image.Rect(0, 0, int(sof.Width), int(sof.Height)))
	for y := 0; y < yCount; y++ {
		for x := 0; x < xCount; x++ {
			temp := ReadMcu(reader, sos, sof, dqts, dhts)
			for i := 0; i < maxH; i++ {
				for j := 0; j < maxW; j++ {
					clr := temp[j][i]
					img.Set(x*maxW+j, y*maxH+i, color.RGBA{R: clr[0], G: clr[1], B: clr[2], A: 255})
				}
			}
		}
	}
	file, err := os.Create("images_new.jpg")
	HandleErr(err)
	err = jpeg.Encode(file, img, &jpeg.Options{Quality: 100})
	HandleErr(err)
}

func ReadMcu(reader io.Reader, sos *Sos, sof *Sof, dqts map[uint8][]uint16, dhts map[uint8]map[string]uint8) [][][]byte {
	res := make(map[uint8][][]float64) // 每种颜色对应的 x,y 处的值
	maxW, maxH := 0, 0
	for type0 := uint8(1); type0 < 4; type0++ { // 必须按 Y Cb Cr 顺序来
		item := sof.Items[type0]
		temp := make([][]float64, item.H*8) // 先定义好大小
		for i := 0; i < len(temp); i++ {
			temp[i] = make([]float64, item.W*8)
		}
		maxW = max(maxW, int(item.W*8))
		maxH = max(maxH, int(item.H*8))
		for y := 0; y < int(item.H); y++ {
			for x := 0; x < int(item.W); x++ {
				data := ReadBlock(reader, type0, sos, dqts[item.DqtID], dhts) // 读取一个 8*8 的块
				for i := 0; i < 8; i++ {                                      // 拷贝 8 次
					copy(temp[y*8+i][x*8:], data[i*8:(i+1)*8])
				}
			}
		}
		res[type0] = temp
	}
	out := make([][][]byte, maxH) // x,y,color
	for i := 0; i < len(out); i++ {
		out[i] = make([][]byte, maxW)
	}
	for y := 0; y < len(out); y++ {
		for x := 0; x < len(out[y]); x++ {
			item := sof.Items[1] // 2 可以一一映射
			cY := res[1][x/(maxW/int(item.W*8))][y/(maxH/int(item.H*8))]
			item = sof.Items[2] // 1 两个映射为 1个
			cB := res[2][x/(maxW/int(item.W*8))][y/(maxH/int(item.H*8))]
			item = sof.Items[3] // 1 两个映射为 1个
			cR := res[3][x/(maxW/int(item.W*8))][y/(maxH/int(item.H*8))]
			out[y][x] = []uint8{HandleCor(cY + 1.402*cR + 128), HandleCor(cY - 0.34414*cB - 0.71414*cR + 128), HandleCor(cY + 1.772*cB + 128)}
		}
	}
	return out
}

func HandleCor(val float64) uint8 {
	if val > 255 {
		return 255
	} else if val < 0 {
		return 0
	} else {
		return uint8(val)
	}
}

var (
	TempData  = ""
	TempIndex = 0
)

func ReadU(reader io.Reader) string {
	if TempIndex >= len(TempData) { // 用完了再取一个
		TempIndex = 0
		data := ReadU8(reader)
		TempData = fmt.Sprintf("%08b", data)
		if data == 0xFF { // 移除后面的 0x00
			if ReadU8(reader) != 0x00 {
				panic("0xFF must end 0x00")
			}
		}
	}
	TempIndex++
	return TempData[TempIndex-1 : TempIndex]
}

func ReadValue(reader io.Reader, size uint8) float64 {
	if size == 0 {
		return 0
	}
	res := ""
	for i := 0; i < int(size); i++ {
		res += ReadU(reader)
	}
	if res[0] == '1' { // 正数
		temp, err := strconv.ParseUint(res, 2, 16)
		HandleErr(err)
		return float64(temp)
	} else {
		str := ""
		for i := 0; i < len(res); i++ { // 负数需要反转 bit 位
			if res[i] == '1' {
				str += "0"
			} else {
				str += "1"
			}
		}
		temp, err := strconv.ParseInt(str, 2, 16)
		HandleErr(err)
		return -float64(temp)
	}
}

//var (
//	Count = 0
//)

func ReadHuffman(reader io.Reader, dht map[string]uint8) uint8 {
	key := ""
	for {
		if res, ok := dht[key]; ok { // 匹配了结束
			//Count++
			//fmt.Println(res, Count)
			return res
		}
		key += ReadU(reader)
		if len(key) > 16 {
			panic("err fmt")
		}
	}
}

var (
	LastDcs = make(map[uint8]float64) // 默认值都是 0
)

func ReadDc(reader io.Reader, type0 uint8, dht map[string]uint8) float64 {
	size := ReadHuffman(reader, dht)
	LastDcs[type0] += ReadValue(reader, size)
	return LastDcs[type0]
}

func ReadAc(reader io.Reader, dht map[string]uint8) uint8 {
	return ReadHuffman(reader, dht)
}

var (
	ZigOrder = []uint8{0, 1, 5, 6, 14, 15, 27, 28,
		2, 4, 7, 13, 16, 26, 29, 42,
		3, 8, 12, 17, 25, 30, 41, 43,
		9, 11, 18, 24, 31, 40, 44, 53,
		10, 19, 23, 32, 39, 45, 52, 54,
		20, 22, 33, 38, 46, 51, 55, 60,
		21, 34, 37, 47, 50, 56, 59, 61,
		35, 36, 48, 49, 57, 58, 62, 63}
)

func ReadBlock(reader io.Reader, type0 uint8, sos *Sos, dqt []uint16, dhts map[uint8]map[string]uint8) []float64 {
	dcID := sos.Items[type0].QhtDcID
	acID := sos.Items[type0].QhtAcID
	dcDht := dhts[dcID|0x00] // 直流高 4 位为 0
	acDht := dhts[acID|0x10] // 交流高 4 位为 1
	//PrintString(dcDht)
	//PrintString(acDht)
	res := make([]float64, 8*8)
	res[0] = float64(ReadDc(reader, type0, dcDht)) // 读取第一个
	idx := 1
	for idx < len(res) {
		val := ReadAc(reader, acDht)
		if val == 0x00 {
			idx = len(res) // 剩下的全部都是 0 了 直接结束
		} else if val == 0xF0 {
			idx += 16 // 连续 16 个 0
		} else {
			//fmt.Println(val, idx, int(val>>4), Count)
			idx += int(val >> 4)                            // 高 4 位个 0
			res[idx] = float64(ReadValue(reader, val&0x0F)) // 外加低 4 位一个值
			idx++
		}
	}
	// 反量化
	for i := 0; i < len(res); i++ {
		res[i] *= float64(dqt[i])
	}
	// 反 zigzag
	temp := make([]float64, 8*8)
	for i := 0; i < len(res); i++ {
		temp[i] = res[ZigOrder[i]] // 纠正位置
	}
	// idct
	res = make([]float64, 8*8)
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			for x := 0; x < 8; x++ {
				for y := 0; y < 8; y++ {
					cosi := math.Cos((2*float64(i) + 1) * math.Pi * float64(x) / 16)
					cosj := math.Cos((2*float64(j) + 1) * math.Pi * float64(y) / 16)
					res[i+j*8] += Cc(x, y) * temp[x+y*8] * cosi * cosj
				}
			} // 注意溢出
			res[i+j*8] /= 4
		}
	}
	return res
}

func Cc(x int, y int) float64 {
	if x == 0 && y == 0 {
		return 0.5
	} else if x == 0 || y == 0 {
		return 1 / math.Sqrt2
	} else {
		return 1
	}
}

func ParseSOS(data []byte) *Sos {
	items := make(map[uint8]*SosItem)
	for i := 0; i < 3; i++ {
		offset := i*2 + 1
		item := &SosItem{
			Type:    data[offset],
			QhtDcID: data[offset+1] >> 4,
			QhtAcID: data[offset+1] & 0x0F,
		}
		items[item.Type] = item
	}
	return &Sos{
		ColorCount: data[0],
		Items:      items,
		Unused:     data[7:],
	}
}

func ParseSOF(data []byte) *Sof {
	items := make(map[uint8]*SofItem)
	for i := 0; i < 3; i++ {
		offset := 6 + i*3
		item := &SofItem{
			Type:  data[offset],
			W:     data[offset+1] >> 4,
			H:     data[offset+1] & 0x0F,
			DqtID: data[offset+2],
		}
		items[item.Type] = item
	}
	return &Sof{
		Accuracy:   data[0],
		Height:     binary.BigEndian.Uint16(data[1:3]),
		Width:      binary.BigEndian.Uint16(data[3:5]),
		ColorCount: data[5],
		Items:      items,
	}
}

func MulTow(val string) string {
	return val + "0"
}

func AddOne(val string) string {
	bs := []byte(val)
	for i := len(bs) - 1; i >= 0; i-- {
		if bs[i] == '0' {
			bs[i] = '1'
			break
		} else {
			bs[i] = '0'
		}
	}
	return string(bs)
}

func HandleDHT(dhts map[uint8]map[string]uint8, data []byte) {
	if len(data) == 0 {
		return
	}
	dht := make(map[string]uint8)
	id := data[0] // 高 4 位表示交流还是直流 0 dc 直流 1 ac 交流  低 4 位表示id
	// 16 byte 高度
	idx := 1 + 16
	last := "" // 默认上个值为 空
	for i := 1; i <= 16; i++ {
		num := int(data[i])
		last = MulTow(last)
		if num == 0 { // 这一层没有值
			continue
		}
		// 第一个特殊处理
		dht[last] = data[idx]
		//fmt.Println(last, data[idx])
		idx++
		// 其他的直接加 1 就行
		for j := 1; j < num; j++ {
			last = AddOne(last)
			dht[last] = data[idx]
			//fmt.Println(last, data[idx])
			idx++
		}
		last = AddOne(last)
	}
	dhts[id] = dht
	HandleDHT(dhts, data[idx:])
}

func HandleDQT(dqts map[uint8][]uint16, data []byte) {
	if len(data) == 0 {
		return
	}
	id := data[0] & 0x0F
	dqt := make([]uint16, 64)
	if data[0]&0xF0 == 0 { // 1bit
		for i := 0; i < 64; i++ {
			dqt[i] = uint16(data[i+1])
		}
		HandleDQT(dqts, data[65:])
	} else { // 2bit
		for i := 0; i < 64; i++ {
			dqt[i] = binary.BigEndian.Uint16(data[i*2+1 : (i+1)*2+1])
		}
		HandleDQT(dqts, data[129:])
	}
	dqts[id] = dqt
}
