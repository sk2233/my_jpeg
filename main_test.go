/*
@author: sk
@date: 2025/5/5
*/
package main

import (
	"fmt"
	"strconv"
	"testing"
)

func TestStr(t *testing.T) {
	fmt.Println(fmt.Sprintf("%08b", uint8(2)))
	data := make(map[int]int)
	data[22] += 22
	fmt.Println(data)
}

func TestNum(t *testing.T) {
	num := int32(-114)
	fmt.Println(uint8(num))
	fmt.Println(strconv.ParseInt("11111111000", 2, 63))
}
