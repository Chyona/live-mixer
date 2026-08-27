package repository

func stringSetKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	return keys
}

func uintSetKeys(set map[uint]struct{}) []uint {
	keys := make([]uint, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	return keys
}
