package util

// Filter returns all the entry matching the function "f" or else return nil
func Filter[T any](list []T, f func(item *T) bool) []*T {
	var ret []*T
	for idx := range list {
		elm := &list[idx]
		if f(elm) {
			ret = append(ret, elm)
		}
	}
	return ret
}
