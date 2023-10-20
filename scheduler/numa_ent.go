// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

//go:build ent

package scheduler

import (
	"math"
	"math/rand"
	"sort"

	"github.com/hashicorp/nomad/client/lib/idset"
	"github.com/hashicorp/nomad/client/lib/numalib"
	"github.com/hashicorp/nomad/client/lib/numalib/hw"
	"github.com/hashicorp/nomad/lib/lang"
	"github.com/hashicorp/nomad/nomad/structs"
)

type coreSelector struct {
	topology       *numalib.Topology
	availableCores *idset.Set[hw.CoreID]
	shuffle        func([]numalib.Core)
}

// Select returns a set of CoreIDs that satisfy the requested core reservations,
// as well as the amount of CPU bandwidth represented by those specific cores.
//
// NUMA preference is available in ent only.
func (cs *coreSelector) Select(ask *structs.Resources) ([]uint16, hw.MHz) {
	switch ask.NUMA.Affinity {
	case structs.PreferNUMA:
		// choose the best cores available
		// soft requirement on numa locality
		return cs.selectPrefer(ask)
	case structs.RequireNUMA:
		// choose the best cores available
		// hard requirement on numa locality
		return cs.selectRequire(ask)
	default:
		// choose cores that fill fragmented nodes
		// no requirement on numa locality
		return cs.selectNone(ask)
	}
}

func (cs *coreSelector) selectNone(ask *structs.Resources) ([]uint16, hw.MHz) {
	// since affinity of none implies we can schedule cores however we wish,
	// use this to our advantage to fill the small fragments on otherwise
	// consumed numa nodes - in doing so leaving large openings for numa tasks

	// group the available cores by node
	byNode := make(map[hw.NodeID][]numalib.Core)
	for _, core := range cs.topology.Cores {
		if cs.availableCores.Contains(core.ID) {
			byNode[core.NodeID] = append(byNode[core.NodeID], core)
		}
	}

	type node struct {
		id        hw.NodeID
		available []numalib.Core
	}

	// turn the map of node->available into a slice we can sort
	nodes := make([]node, 0, len(byNode))
	lang.WalkMap(byNode, func(id hw.NodeID, cores []numalib.Core) bool {
		nodes = append(nodes, node{
			id:        id,
			available: cores,
		})
		return true
	})

	// sort the slice of per-node available cores by size
	sort.Slice(nodes, func(x, y int) bool {
		X := len(nodes[x].available)
		Y := len(nodes[y].available)
		return X < Y
	})

	// keep track of the cores we select
	selection := make([]numalib.Core, 0, ask.Cores)

	// gobble up cores going by increasing fragment size
	for _, n := range nodes {
		for _, core := range n.available {
			selection = append(selection, core)
			if len(selection) == ask.Cores {
				return cs.chosen(selection)
			}
		}
	}

	// there were insufficient cores available
	return nil, 0
}

func (cs *coreSelector) selectPrefer(ask *structs.Resources) ([]uint16, hw.MHz) {
	// The strategy here is that we create a slice of all available cores (from
	// all nodes), for each node. Store this in a map of NodeID -> []Core. Then
	// for each NodeID we sort its slice of cores based on the distance of each
	// core to that Node. From here we select the first N cores from each list
	// where N is the number of cores requested by the task, and compute the
	// NUMA cost for that sub slice of cores. So now we have a minimal possible
	// cost for each Node, and we simply need to select the sub slice of
	// minimal cost.
	//
	// Ignoring efficiency, a naive way to think about this is simply to compute
	// the complete set of possible combinations of size N cores, compute the
	// cost of each combination, and select the minimal one. The problem with
	// actually using this approach is that there are 2^N possible combinations
	// of a set of size N. On a machine with 192 cores you would have to compute
	// the cost of ~ 6 octodecillion possible combinations, which nobody has
	// time for.
	//
	// See NMD-187 for some helpful diagrams.

	// fast path exit if there are not enough cores for the ask
	if cs.availableCores.Size() < ask.Cores {
		return nil, 0
	}

	// first create a slice of available we can work with
	available := make([]numalib.Core, 0, cs.availableCores.Size())
	for _, core := range cs.topology.Cores {
		if cs.availableCores.Contains(core.ID) {
			available = append(available, core)
		}
	}

	// for each node, associate with a list of available cores
	byNode := make(map[hw.NodeID][]numalib.Core)
	_ = cs.topology.NodeIDs.ForEach(func(nodeID hw.NodeID) error {
		byNode[nodeID] = make([]numalib.Core, len(available))
		copy(byNode[nodeID], available)
		return nil
	})

	// sort the available cores by distance to each node
	for nodeID, cores := range byNode {
		sort.Slice(cores, func(x, y int) bool {
			coreX := cores[x]
			coreY := cores[y]
			distX := cs.topology.NodeDistance(nodeID, coreX)
			distY := cs.topology.NodeDistance(nodeID, coreY)
			if distX < distY {
				return true
			} else if distX > distY {
				return false
			}
			// otherwise sort by core id
			return coreX.ID < coreY.ID
		})
	}

	// select the set of cores with lowest overall numa distance cost
	var bestCost = math.MaxInt
	var bestCores []numalib.Core
	lang.WalkMap(byNode, func(_ hw.NodeID, cores []numalib.Core) bool {
		selection := cores[0:ask.Cores]
		cost := cs.cost(selection)
		if cost < bestCost {
			bestCores = selection
			bestCost = cost
		}
		return true
	})

	// return our selection
	return cs.chosen(bestCores)
}

func (cs *coreSelector) cost(cores []numalib.Core) int {
	total := 0
	for a := 0; a < len(cores); a++ {
		for b := a + 1; b < len(cores); b++ {
			nodeA, nodeB := cores[a].NodeID, cores[b].NodeID
			distance := cs.topology.Distances[nodeA][nodeB]
			total += int(distance)
		}
	}
	return total
}

func (cs *coreSelector) selectRequire(ask *structs.Resources) ([]uint16, hw.MHz) {
	// Use best-fit to find a group of cores on a single numa node at least as
	// large as the ask cores. The actual cores are selected at random to help
	// avoid PFNR on the core resource. If no such group exists return (nil, 0).

	// group the available cores by node
	byNode := make(map[hw.NodeID][]numalib.Core)
	for _, core := range cs.topology.Cores {
		if cs.availableCores.Contains(core.ID) {
			byNode[core.NodeID] = append(byNode[core.NodeID], core)
		}
	}

	// find the smallest group that is large enough for the core ask
	var smallest []numalib.Core
	lang.WalkMap(byNode, func(_ hw.NodeID, cores []numalib.Core) bool {
		if size := len(cores); size >= ask.Cores {
			if sm := len(smallest); sm == 0 || size < sm {
				smallest = cores
			}
		}
		return true
	})

	// there was no group large enough for the core ask
	if len(smallest) == 0 {
		return nil, 0
	}

	// shuffle the cores in our chosen group
	cs.shuffle(smallest)
	return cs.chosen(smallest[0:ask.Cores])
}

// convert the chosen cores into the expected core id and frequency units
func (cs *coreSelector) chosen(cores []numalib.Core) ([]uint16, hw.MHz) {
	var (
		chosenIDs []uint16
		chosenMHz hw.MHz
	)
	for _, core := range cores {
		chosenIDs = append(chosenIDs, uint16(core.ID))
		chosenMHz += core.MHz()
	}
	return chosenIDs, chosenMHz
}

// randomize the cores so we can at least try to mitigate PFNR problems
func randomizeCores(cores []numalib.Core) {
	rand.Shuffle(len(cores), func(x, y int) {
		cores[x], cores[y] = cores[y], cores[x]
	})
}
