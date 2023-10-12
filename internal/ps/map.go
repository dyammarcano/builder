// Package ps Fully persistent data structures. A persistent data structure is a data
// structure that always preserves the previous version of itself when
// it is modified. Such data structures are effectively immutable,
// as their operations do not update the structure in-place, but instead
// always yield a new structure.
//
// Persistent
// data structures typically share structure among themselves.  This allows
// operations to avoid copying the entire data structure.
package ps

import (
	"fmt"
	"strings"
)

// Any is a shorthand for Go's verbose interface{} type.
type Any interface{}

// A Map associates unique keys (type string) with values (type Any).
type Map interface {
	// IsNil returns true if the Map is empty
	IsNil() bool

	// Set returns a new map in which key and value are associated.
	// If the key didn't exist before, it's created; otherwise, the
	// associated value is changed.
	// This operation is O(log N) in the number of keys.
	Set(key string, value Any) Map

	// Delete returns a new map with the association for key, if any, removed.
	// This operation is O(log N) in the number of keys.
	Delete(key string) Map

	// Lookup returns the value associated with a key, if any.  If the key
	// exists, the second return value is true; otherwise, false.
	// This operation is O(log N) in the number of keys.
	Lookup(key string) (Any, bool)

	// Size returns the number of key value pairs in the map.
	// This takes O(1) time.
	Size() int

	// ForEach executes a callback on each key value pair in the map.
	ForEach(f func(key string, val Any))

	// Keys returns a slice with all keys in this map.
	// This operation is O(N) in the number of keys.
	Keys() []string

	String() string
}

// Immutable (i.e. persistent) associative array
const childCount = 8
const shiftSize = 3

type tree struct {
	count    int
	hash     uint64 // hash of the key (used for tree balancing)
	key      string
	value    Any
	children [childCount]*tree
}

var nilMap = &tree{}

// Recursively set nilMap's subtrees to point at itself.
// This eliminates all nil pointers in the map structure.
// All map nodes are created by cloning this structure, so
// they avoid the problem too.
func init() {
	for i := range nilMap.children {
		nilMap.children[i] = nilMap
	}
}

// NewMap allocates a new, persistent map from strings to values of
// any type.
// This is currently implemented as a path-copying binary tree.
func NewMap() Map {
	return nilMap
}

func (t *tree) IsNil() bool {
	return t == nilMap
}

// clone returns an exact duplicate of a tree node
func (t *tree) clone() *tree {
	var m tree
	m = *t
	return &m
}

// constants for FNV-1a hash algorithm
const (
	offset64 uint64 = 14695981039346656037
	prime64  uint64 = 1099511628211
)

// hashKey returns a hash code for a given string
func hashKey(key string) uint64 {
	hash := offset64
	for _, codepoint := range key {
		hash ^= uint64(codepoint)
		hash *= prime64
	}
	return hash
}

// Set returns a new map similar to this one but with key and value
// associated.  If the key didn't exist, it's created; otherwise, the
// associated value is changed.
func (t *tree) Set(key string, value Any) Map {
	hash := hashKey(key)
	return setLowLevel(t, hash, hash, key, value)
}

func setLowLevel(self *tree, partialHash, hash uint64, key string, value Any) *tree {
	if self.IsNil() { // an empty tree is easy
		m := self.clone()
		m.count = 1
		m.hash = hash
		m.key = key
		m.value = value
		return m
	}

	if hash != self.hash {
		m := self.clone()
		i := partialHash % childCount
		m.children[i] = setLowLevel(self.children[i], partialHash>>shiftSize, hash, key, value)
		recalculateCount(m)
		return m
	}

	// replacing a key's previous value
	m := self.clone()
	m.value = value
	return m
}

// modifies a map by recalculating its key count based on the counts
// of its subtrees
func recalculateCount(m *tree) {
	count := 0
	for _, t := range m.children {
		count += t.Size()
	}
	m.count = count + 1 // add one to count ourselves
}

func (t *tree) Delete(key string) Map {
	hash := hashKey(key)
	newMap, _ := deleteLowLevel(t, hash, hash)
	return newMap
}

func deleteLowLevel(self *tree, partialHash, hash uint64) (*tree, bool) {
	// empty trees are easy
	if self.IsNil() {
		return self, false
	}

	if hash != self.hash {
		i := partialHash % childCount
		child, found := deleteLowLevel(self.children[i], partialHash>>shiftSize, hash)
		if !found {
			return self, false
		}
		newMap := self.clone()
		newMap.children[i] = child
		recalculateCount(newMap)
		return newMap, true // ? this wasn't in the original code
	}

	// we must delete our own node
	if self.isLeaf() { // we have no children
		return nilMap, true
	}
	/*
	   if self.subtreeCount() == 1 { // only one subtree
	       for _, t := range self.children {
	           if t != nilMap {
	               return t, true
	           }
	       }
	       panic("Tree with 1 subtree actually had no subtrees")
	   }
	*/

	// find a node to replace us
	i := -1
	size := -1
	for j, t := range self.children {
		if t.Size() > size {
			i = j
			size = t.Size()
		}
	}

	// make chosen leaf smaller
	replacement, child := self.children[i].deleteLeftmost()
	newMap := replacement.clone()
	for j := range self.children {
		if j == i {
			newMap.children[j] = child
		} else {
			newMap.children[j] = self.children[j]
		}
	}
	recalculateCount(newMap)
	return newMap, true
}

// delete the leftmost node in a tree returning the node that
// was deleted and the tree left over after its deletion
func (t *tree) deleteLeftmost() (*tree, *tree) {
	if t.isLeaf() {
		return t, nilMap
	}

	for i, c := range t.children {
		if c != nilMap {
			deleted, child := c.deleteLeftmost()
			newMap := t.clone()
			newMap.children[i] = child
			recalculateCount(newMap)
			return deleted, newMap
		}
	}
	panic("Tree isn't a leaf but also had no children. How does that happen?")
}

// isLeaf returns true if this is a leaf node
func (t *tree) isLeaf() bool {
	return t.Size() == 1
}

// returns the number of child subtrees we have
func (t *tree) subtreeCount() int {
	count := 0
	for _, c := range t.children {
		if c != nilMap {
			count++
		}
	}
	return count
}

func (t *tree) Lookup(key string) (Any, bool) {
	hash := hashKey(key)
	return lookupLowLevel(t, hash, hash)
}

func lookupLowLevel(self *tree, partialHash, hash uint64) (Any, bool) {
	if self.IsNil() { // an empty tree is easy
		return nil, false
	}

	if hash != self.hash {
		i := partialHash % childCount
		return lookupLowLevel(self.children[i], partialHash>>shiftSize, hash)
	}

	// we found it
	return self.value, true
}

func (t *tree) Size() int {
	return t.count
}

func (t *tree) ForEach(f func(key string, val Any)) {
	if t.IsNil() {
		return
	}

	// ourself
	f(t.key, t.value)

	// children
	for _, c := range t.children {
		if c != nilMap {
			c.ForEach(f)
		}
	}
}

func (t *tree) Keys() []string {
	keys := make([]string, t.Size())
	i := 0
	t.ForEach(func(k string, v Any) {
		keys[i] = k
		i++
	})
	return keys
}

// make it easier to display maps for debugging
func (t *tree) String() string {
	keys := t.Keys()

	var builder strings.Builder
	builder.WriteString("{")
	for _, key := range keys {
		val, _ := t.Lookup(key)
		_, err := fmt.Fprintf(&builder, "%s: %s, ", key, val)
		if err != nil {
			return ""
		}
	}
	builder.WriteString("}\n")

	return builder.String()
}
