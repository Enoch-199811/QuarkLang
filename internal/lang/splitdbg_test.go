package lang

import (
	"fmt"
	"strings"
	"testing"
)

func TestSplitDbg2(t *testing.T) {
	segs := []string{}
	for k := 1; k <= 11; k++ {
		segs = append(segs, fmt.Sprintf("x%04d#seg%d", k, k))
	}
	lit := "x0000#start" + "/rn-" + strings.Join(segs, "/rn-")
	parts := strings.Split(lit, "/rn-")
	lst := NewList()
	for _, p := range parts {
		lst.Append(StrV(p))
	}
	fmt.Println("size:", lst.Size())
	for i := 0; i < lst.Size(); i++ {
		v, _ := lst.Get(i)
		fmt.Printf("item%d=%q ptr=%x\n", i, v.Str(), v.ptr)
	}
}
