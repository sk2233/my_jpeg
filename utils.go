/*
@author: sk
@date: 2025/5/4
*/
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

func HandleErr(err error) {
	if err != nil {
		panic(err)
	}
}

func ReadU8(reader io.Reader) uint8 {
	return ReadByte(reader, 1)[0]
}

func ReadU16(reader io.Reader) uint16 {
	res := ReadByte(reader, 2)
	return binary.BigEndian.Uint16(res)
}

func ReadByte(reader io.Reader, size int) []byte {
	res := make([]byte, size)
	_, err := reader.Read(res)
	HandleErr(err)
	return res
}

// 对于 0xFF 0x00 的情况会算做一个
func ReadByteWithSkip(reader io.Reader, size int) []byte {
	res := make([]byte, 0, size)
	for len(res) < size {
		temp := make([]byte, size-len(res))
		_, err := reader.Read(temp)
		HandleErr(err)
		for i := 0; i < len(temp); i++ {
			res = append(res, temp[i])
			if temp[i] == 0xFF {
				i++ // 跳过后面的 0x00
			}
		}
	}
	return res
}

func PrintString(val any) {
	bs, err := json.Marshal(val)
	HandleErr(err)
	fmt.Println(string(bs))
}
