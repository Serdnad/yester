package main

import (
	"fmt"
	"strings"
)

type TestNode struct {
	Key      string
	Test     *Test
	Config   *TestGroup
	Children []*TestNode
}

func addChildNode(root, child *TestNode, parentKey string) bool {
	if parentKey == root.Key {
		root.Children = append(root.Children, child)
		return true
	}

	for _, node := range root.Children {
		if addChildNode(node, child, parentKey) {
			return true
		}
	}

	return false
}

func printNode(node *TestNode, depth int) {
	fmt.Printf("%s- %v\n", strings.Repeat(" ", depth), node.Key)
}

func printGraph(root *TestNode) {
	_printGraph(root, 0)
}

func _printGraph(root *TestNode, depth int) {
	printNode(root, depth)

	for _, child := range root.Children {
		_printGraph(child, depth+1)
	}
}
