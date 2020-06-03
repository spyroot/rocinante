/**

	TODO port LPM
	https://doc.dpdk.org/guides/prog_guide/lpm_lib.html
 */
package radix

import (
	"strings"
)

func longestMatch(n, m string) int {

	max := len(n)
	mLen := len(m)

	if mLen < max {
		max = mLen
	}

	var i = 0
	for ; i < max; i++ {
		if n[i] != m[i] {
			break
		}
	}

	return i
}

type Callback func(s string, v interface{}) bool

type edges []edge

// leafNode is used to represent a value
type leafNode struct {
	key string
	val interface{}
}

/**

   Radix tree implements
   https://en.wikipedia.org/wiki/Radix_tree
 */
type Tree struct {
	root      *Node
	size      int
	predicate Callback
}

/**
   Return a new tree and it initialized to nil
 */
func New() *Tree {
	return NewFromMap(nil)
}

/*
	Create a new tree from map interface
 */
func NewFromMap(m map[string]interface{}) *Tree {
	t := &Tree{root: &Node{}}
	for k, v := range m {
		t.Insert(k, v)
	}
	return t
}

func (tree *Tree) Len() int {
	return tree.size
}

/**
    Add node
 */
func (tree *Tree) addNode(s string, v interface{}, parent *Node)  {
	parent.add(edge {
		bits: s[0],
		node: &Node {
			leaf: &leafNode {s, v},
			prefix: s,
		},
	})
	tree.size++
}

/**

 */
func (tree *Tree) Insert(s string, v interface{}) (interface{}, bool) {

	var parent *Node
	subTree := tree.root
	query := s

	for {
		// case one
		if len(query) == 0 {
			// update case
			if subTree.isLeaf() {
				old := subTree.leaf.val
				subTree.leaf.val = v
				return old, true
			}
			// new case case
			subTree.leaf = &leafNode{s, v}
			tree.size++
			return nil, false
		}

		// look for an edge
		parent = subTree
		subTree = subTree.getEdge(query[0])

		// add new ode and edge
		if subTree == nil {
			tree.addNode(s, v, subTree)
			return nil, false
		}

		// common prefix of the search key on match
		common := longestMatch(query, subTree.prefix)
		if common == len(subTree.prefix) {
			// advance to a len of common and skip
			query = query[common:]
			continue
		}

		// case when we need create a new node
		tree.size++
		child := &Node{prefix: query[:common]}
		parent.update(query[0], child)

		// Restore the existing Node
		child.add(edge{subTree.prefix[common], subTree})
		subTree.prefix = subTree.prefix[common:]

		// a new leaf Node  add to this Node
		leaf := &leafNode{s, v}
		query = query[common:]
		if len(query) == 0 {
			child.leaf = leaf
			return nil, false
		}

		// Create a new edge for the Node
		child.add(edge {
			bits: query[0],
			node: &Node{leaf:   leaf, prefix: query,
			},
		})
		return nil, false
	}
}

// remove is used to delete a key, returning the previous
// value and if it was deleted
func (tree *Tree) removeNode(n *Node, parent *Node, label byte) (interface{}, bool)  {

	leaf := n.leaf
	n.leaf = nil
	tree.size--

	if parent != nil && len(n.edges) == 0 {
		parent.delEdge(label)
	}

	// Check if we should merge this Node
	if n != tree.root && len(n.edges) == 1 {
		n.join()
	}

	return leaf.val, true
}

/**

 */
func (tree *Tree) remove(s string) (interface{}, bool) {

	var parent *Node
	var bits byte

	subTree := tree.root
	query := s

	searchLen := len(query)

	for {

		if searchLen == 0 {
			if !subTree.isLeaf() {
				break
			}
			return tree.removeNode(subTree, parent, bits)
		}

		parent = subTree
		bits = query[0]
		subTree = subTree.getEdge(bits)
		if subTree == nil || !strings.HasPrefix(query, subTree.prefix) {
			break
		}

		// advance query forward
		query = query[len(subTree.prefix):]
	}

	return nil, false
}

/**
    remove prefix and sub tree
 */
func (tree *Tree) RemovePrefix(s string) int {
	return tree.removePrefix(nil, tree.root, s)
}

/**
    Recursive delete
 */
func (tree *Tree) removePrefix(parent, n *Node, prefix string) int {

	prefixLen := len(prefix)

	if prefixLen == 0 {
		// Remove the leaf Node
		subTreeSize := 0
		//recursively walk from all edges of the Node to be deleted
		preorderWalk(n,
			func(s string, v interface{}) bool {
				subTreeSize++
				return false
			})

		if n.isLeaf() {
			n.leaf = nil
		}

		n.edges = nil // deletes the entire subtree

		// Merge predicate
		if parent != nil && parent != tree.root && len(parent.edges) == 1 && !parent.isLeaf() {
			parent.join()
		}

		tree.size -= subTreeSize
		return subTreeSize
	}

	child := n.getEdge(prefix[0])
	// case one
	if child == nil {
		return 0
	}
	// two
	if !strings.HasPrefix(child.prefix, prefix) && !strings.HasPrefix(prefix, child.prefix) {
		return 0
	}

	childLen := len(child.prefix)
	if childLen > prefixLen {
		prefix = prefix[prefixLen:]
	} else {
		prefix = prefix[childLen:]
	}

	return tree.removePrefix(n, child, prefix)
}

/**

 */
func (n *Node) join() {

	e := n.edges[0]
	child := e.node

	n.prefix = n.prefix + child.prefix
	n.leaf = child.leaf
	n.edges = child.edges
}

/**
	lookup key if, if found return key and true
 */
func (tree *Tree) lookup(s string) (interface{}, bool) {

	subTree := tree.root
	query := s

	for {

		// base case if query is zero and node is leaf we found
		if len(query) == 0 {
			if subTree.isLeaf() {
				return subTree.leaf.val, true
			}
			break
		}

		// look for an edge and adjust subTree
		subTree = subTree.getEdge(query[0])
		if subTree == nil {
			break
		}

		// if we found common prefix, shift query and keep searching
		// otherwise break
		if strings.HasPrefix(query, subTree.prefix) {
			query = query[len(subTree.prefix):]
		} else {
			break
		}
	}

	return nil, false
}

/**
	Longest prefix match
 */
func (tree *Tree) LongestPrefix(s string) (string, interface{}, bool) {

	var lastLeaf *leafNode = nil
	subTree := tree.root
	query := s

	for {

		// update lastLeaf leaf
		if subTree.isLeaf() {
			lastLeaf = subTree.leaf
		}

		// base case
		if len(query) == 0 {
			break
		}

		// check what sub tree to visit on current position 0
		subTree = subTree.getEdge(query[0])
		if subTree == nil {
			break
		}

		// check common prefix ,  adjust query and keep going
		if strings.HasPrefix(query, subTree.prefix) {
			query = query[len(subTree.prefix):]
		} else {
			break
		}

	}

	// return key , value, true , we found leaf node
	if lastLeaf != nil {
		return lastLeaf.key, lastLeaf.val, true
	}

	return "", nil, false
}

/**

 */
func (tree *Tree) Min() (string, interface{}, bool) {

	subTree := tree.root

	for {

		// base case
		if subTree.isLeaf() {
			return subTree.leaf.key, subTree.leaf.val, true
		}

		if len(subTree.edges) == 0 {
			break
		}

		// update subtree
		subTree = subTree.edges[0].node
	}

	return "", nil, false
}


/*
	Maximum is used to return the maximum value in the tree
 */
func (tree *Tree) Max() (string, interface{}, bool) {

	subTree := tree.root

	for {

		if num := len(subTree.edges); num > 0 {
			subTree = subTree.edges[num - 1].node
			continue
		}

		if subTree.isLeaf() {
			return subTree.leaf.key, subTree.leaf.val, true
		}

		break
	}

	return "", nil, false
}

/*

 */
func (tree *Tree) Walk() {
	preorderWalk(tree.root, tree.predicate)
}

/**

 */
func (tree *Tree) WalkPrefix(prefix string) {

	subTree := tree.root
	search := prefix

	for {
		// base case
		if len(search) == 0 {
			preorderWalk(subTree, tree.predicate)
			return
		}
		// choose
		subTree = subTree.getEdge(search[0])
		if subTree == nil {
			break
		}
		// Consume the search prefix
		if strings.HasPrefix(search, subTree.prefix) {
			search = search[len(subTree.prefix):]
		} else if strings.HasPrefix(subTree.prefix, search) {
			// Child may be under our search prefix
			preorderWalk(subTree, tree.predicate)
			return
		} else {
			break
		}
	}
}

/*
   Walk along a tree and visit sub-tree
   from a give root.
 */
func (tree *Tree) PreorderWalk(path string) {

	subTree := tree.root
	query := path

	for {

		// visit the leaf values if any
		if subTree.leaf != nil && tree.predicate(subTree.leaf.key, subTree.leaf.val) {
			return
		}
		if len(query) == 0 {
			return
		}
		// Look for an edge
		subTree = subTree.getEdge(query[0])
		if subTree == nil {
			return
		}
		// adjust the query prefix and keep going
		if strings.HasPrefix(query, subTree.prefix) {
			query = query[len(subTree.prefix):]
		} else {
			break
		}
	}

}

/**
  Pre order walk ,  it return true
  if lookup should be aborted
*/
func preorderWalk(node *Node, predicate Callback) bool {
	// base case,  if node is not a leaf and predicate is true we done
	if node.leaf != nil && predicate(node.leaf.key, node.leaf.val) {
		return true
	}
	// otherwise choose edge and recurse
	for _, e := range node.edges {
		if preorderWalk(e.node, predicate) {
			return true
		}
	}
	return false
}
