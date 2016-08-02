package dsync

import (
	"errors"
)

var n int
var nodes []string

func SetNodes(nodeList []string) error {

	if n != 0 {
		return errors.New("Cannot reinitialize dsync package")
	} else if len(nodeList) < 4 {
		return errors.New("Dsync not designed for less than 4 nodes")
	} else if len(nodeList) > 16 {
		return errors.New("Dsync not designed for more than 16 nodes")
	}

	nodes = make([]string, len(nodeList))
	copy(nodes, nodeList[:])

	n = len(nodes)

	return nil
}
