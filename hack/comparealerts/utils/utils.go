package utils

import (
	"strings"
)

func TrimWithOnlySpaces(str string) (retStr string) {
	strFields := strings.Fields(str)
	retStr = strings.Join(strFields, " ")
	return
}
