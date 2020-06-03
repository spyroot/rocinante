package radix

import "sort"

// edge is used to represent an edge Node
type edge struct {
	bits   byte
	node  *Node
}

func (e edges) Len() int {
	return len(e)
}

func (e edges) Less(i, j int) bool {
	return e[i].bits < e[j].bits
}

func (e edges) Swap(i, j int) {
	e[i], e[j] = e[j], e[i]
}

func (e edges) Sort() {
	sort.Sort(e)
}

