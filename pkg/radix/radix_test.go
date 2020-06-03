package radix

import (
	"reflect"
	"testing"
)

func TestTree_LongestPrefix(t *testing.T) {
	type fields struct {
		root      *Node
		size      int
		predicate Callback
	}
	type args struct {
		s string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   string
		want1  interface{}
		want2  bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := &Tree{
				root:      tt.fields.root,
				size:      tt.fields.size,
				predicate: tt.fields.predicate,
			}
			got, got1, got2 := tree.LongestPrefix(tt.args.s)
			if got != tt.want {
				t.Errorf("LongestPrefix() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf("LongestPrefix() got1 = %v, want %v", got1, tt.want1)
			}
			if got2 != tt.want2 {
				t.Errorf("LongestPrefix() got2 = %v, want %v", got2, tt.want2)
			}
		})
	}
}

func TestTree_Max(t *testing.T) {
	type fields struct {
		root      *Node
		size      int
		predicate Callback
	}
	tests := []struct {
		name   string
		fields fields
		want   string
		want1  interface{}
		want2  bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := &Tree{
				root:      tt.fields.root,
				size:      tt.fields.size,
				predicate: tt.fields.predicate,
			}
			got, got1, got2 := tree.Max()
			if got != tt.want {
				t.Errorf("Max() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf("Max() got1 = %v, want %v", got1, tt.want1)
			}
			if got2 != tt.want2 {
				t.Errorf("Max() got2 = %v, want %v", got2, tt.want2)
			}
		})
	}
}

func TestTree_Min(t *testing.T) {
	type fields struct {
		root      *Node
		size      int
		predicate Callback
	}
	tests := []struct {
		name   string
		fields fields
		want   string
		want1  interface{}
		want2  bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := &Tree{
				root:      tt.fields.root,
				size:      tt.fields.size,
				predicate: tt.fields.predicate,
			}
			got, got1, got2 := tree.Min()
			if got != tt.want {
				t.Errorf("Min() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf("Min() got1 = %v, want %v", got1, tt.want1)
			}
			if got2 != tt.want2 {
				t.Errorf("Min() got2 = %v, want %v", got2, tt.want2)
			}
		})
	}
}

func TestTree_RemovePrefix(t *testing.T) {
	type fields struct {
		root      *Node
		size      int
		predicate Callback
	}
	type args struct {
		s string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   int
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := &Tree{
				root:      tt.fields.root,
				size:      tt.fields.size,
				predicate: tt.fields.predicate,
			}
			if got := tree.RemovePrefix(tt.args.s); got != tt.want {
				t.Errorf("RemovePrefix() = %v, want %v", got, tt.want)
			}
		})
	}
}


func TestTree_lookup(t *testing.T) {
	type fields struct {
		root      *Node
		size      int
		predicate Callback
	}
	type args struct {
		s string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   interface{}
		want1  bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := &Tree{
				root:      tt.fields.root,
				size:      tt.fields.size,
				predicate: tt.fields.predicate,
			}
			got, got1 := tree.lookup(tt.args.s)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("lookup() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("lookup() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestTree_remove(t *testing.T) {
	type fields struct {
		root      *Node
		size      int
		predicate Callback
	}
	type args struct {
		s string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   interface{}
		want1  bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := &Tree{
				root:      tt.fields.root,
				size:      tt.fields.size,
				predicate: tt.fields.predicate,
			}
			got, got1 := tree.remove(tt.args.s)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("remove() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("remove() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestTree_removeNode(t *testing.T) {
	type fields struct {
		root      *Node
		size      int
		predicate Callback
	}
	type args struct {
		n      *Node
		parent *Node
		label  byte
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   interface{}
		want1  bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := &Tree{
				root:      tt.fields.root,
				size:      tt.fields.size,
				predicate: tt.fields.predicate,
			}
			got, got1 := tree.removeNode(tt.args.n, tt.args.parent, tt.args.label)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("removeNode() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("removeNode() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestTree_removePrefix(t *testing.T) {
	type fields struct {
		root      *Node
		size      int
		predicate Callback
	}
	type args struct {
		parent *Node
		n      *Node
		prefix string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   int
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := &Tree{
				root:      tt.fields.root,
				size:      tt.fields.size,
				predicate: tt.fields.predicate,
			}
			if got := tree.removePrefix(tt.args.parent, tt.args.n, tt.args.prefix); got != tt.want {
				t.Errorf("removePrefix() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_longestMatch(t *testing.T) {
	type args struct {
		n string
		m string
	}
	tests := []struct {
		name string
		args args
		want int
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := longestMatch(tt.args.n, tt.args.m); got != tt.want {
				t.Errorf("longestMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_preorderWalk(t *testing.T) {
	type args struct {
		node      *Node
		predicate Callback
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := preorderWalk(tt.args.node, tt.args.predicate); got != tt.want {
				t.Errorf("preorderWalk() = %v, want %v", got, tt.want)
			}
		})
	}
}