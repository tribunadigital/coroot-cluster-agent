package common

import (
	"cmp"
	"sort"
)

func TopN[T any](s []T, n int, less func(a, b T) bool) []T {
	if len(s) <= n {
		return s
	}
	sort.Slice(s, func(i, j int) bool { return less(s[i], s[j]) })
	return s[:n]
}

func TopNMapByValue[K comparable, V cmp.Ordered](m map[K]V, n int) map[K]V {
	return TopNMap(m, n, func(v V) V { return v })
}

func TopNMap[K comparable, V any, S cmp.Ordered](m map[K]V, n int, val func(V) S) map[K]V {
	if len(m) <= n {
		return m
	}
	type kv struct {
		k K
		v V
		s S
	}
	items := make([]kv, 0, len(m))
	for k, v := range m {
		items = append(items, kv{k, v, val(v)})
	}
	items = TopN(items, n, func(a, b kv) bool { return a.s > b.s })
	res := make(map[K]V, len(items))
	for _, it := range items {
		res[it.k] = it.v
	}
	return res
}
