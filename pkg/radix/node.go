package radix

import "sort"

type Node struct {

	leaf *leafNode

	// prefix is the common prefix we ignore
	prefix string

	// sorted edges
	edges edges
}

func (n *Node) isLeaf() bool {
	return n.leaf != nil
}

/*
	Adds edge
 */
func (n *Node) add(e edge) {
	n.edges = append(n.edges, e)
	n.edges.Sort()
}

/**
	Update
 */
func (n *Node) update(bits byte, node *Node) {

	num := len(n.edges)

	// find index
	i := sort.Search(num,
		func(i int) bool {
			return n.edges[i].bits >= bits
		})

	// if same bits but index different update i'th index
	if n.edges[i].bits == bits && i < num {
		n.edges[i].node = node
		return
	}

	panic("replacing missing edge")
}

/*
	Return edge
*/
func (n *Node) getEdge(label byte) *Node {

	num := len(n.edges)
	idx := sort.Search(num, func(i int) bool {
		return n.edges[i].label >= label
	})
	if idx < num && n.edges[idx].label == label {
		return n.edges[idx].node
	}
	return nil
}

func (n *Node) delEdge(label byte) {
	num := len(n.edges)
	idx := sort.Search(num, func(i int) bool {
		return n.edges[i].label >= label
	})
	if idx < num && n.edges[idx].label == label {
		copy(n.edges[idx:], n.edges[idx+1:])
		n.edges[len(n.edges)-1] = edge{}
		n.edges = n.edges[:len(n.edges)-1]
	}
}
