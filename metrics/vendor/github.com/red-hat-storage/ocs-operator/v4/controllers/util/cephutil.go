package util

import "math"

const (
	pgCountPerOSD = 600
	maxPGUnitSize = 128
)

func GetPGBaseUnitSize(osdCount int) int {
	// leaving out 16 PGs as a buffer
	totalPGCount := (pgCountPerOSD * osdCount) - 16
	// 9 = 5 (block pool consumers) + 4 (ceph fs underlying block pool)
	pgUnitSize := math.Min(float64(totalPGCount/9), maxPGUnitSize)
	// Round to the nearest power of 2
	pgUnitSize = math.Pow(2, math.Floor(math.Log2(pgUnitSize)))
	return int(pgUnitSize)
}
