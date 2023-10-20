// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

//go:build ent

package scheduler

import (
	"slices"
	"sort"
	"testing"

	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/client/lib/idset"
	"github.com/hashicorp/nomad/client/lib/numalib"
	"github.com/hashicorp/nomad/client/lib/numalib/hw"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/shoenig/test/must"
)

// orderCores is an implementation of "shuffle" that orders the cores by
// coreID so that we can make deterministic assertions in tests
func orderCores(cores []numalib.Core) {
	sort.Slice(cores, func(x, y int) bool {
		X := cores[x]
		Y := cores[y]
		return X.ID < Y.ID
	})
}

// a reminder of the mock topologies
//
// MockBasicTopology:
// - 1 socket, 1 NUMA node
// - 4 cores @ 3500 MHz (14,000 MHz total)
//
// MockWorkstationTopology:
// - 2 socket, 2 NUMA node (200% penalty)
// - 16 cores / 32 threads @ 3000 MHz (96,000 MHz total)
// - node0: 1, 3, 5, 7, 9, 11, 13, 15, 17, 19, 21, 23, 25, 27, 29, 31
// - node1: 0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22, 24, 26, 28, 30
//
// MockR6aTopology:
// - 2 socket, 4 NUMA node
// - 120% penalty for intra socket, 320% penalty for cross socket
// - 192 logical cores @ 3731 MHz (716_362 MHz total)
// - node0:  0-23,  96-119 (socket 0)
// - node1: 24-47, 120-143 (socket 0)
// - node2: 48-71, 144-167 (socket 1)
// - node3: 72-95, 168-191 (socket 1)

type numaTestCase struct {
	name      string
	topology  *numalib.Topology
	available []hw.CoreID
	cores     int
	expCores  []uint16
	expMHz    hw.MHz
}

func TestNUMA_Select_none(t *testing.T) {
	ci.Parallel(t)

	cases := []numaTestCase{
		{
			name:      "vm: no cores available",
			topology:  structs.MockBasicTopology(),
			available: []hw.CoreID{},
			cores:     1,
			expCores:  nil,
			expMHz:    0,
		},
		{
			name:      "vm: one core available",
			topology:  structs.MockBasicTopology(),
			available: []hw.CoreID{2},
			cores:     1,
			expCores:  []uint16{2},
			expMHz:    3_500,
		},
		{
			name:      "vm: insufficient cores available",
			topology:  structs.MockBasicTopology(),
			available: []hw.CoreID{1, 3},
			cores:     3,
			expCores:  nil,
			expMHz:    0,
		},
		{
			name:      "vm: select two from four",
			topology:  structs.MockBasicTopology(),
			available: []hw.CoreID{0, 1, 2, 3},
			cores:     2,
			expCores:  []uint16{0, 1},
			expMHz:    3_500 * 2,
		},
		{
			name:      "vm: fill remaining",
			topology:  structs.MockBasicTopology(),
			available: []hw.CoreID{0, 1, 3},
			cores:     3,
			expCores:  []uint16{0, 1, 3},
			expMHz:    3_500 * 3,
		},
		{
			name:      "ws: no cores available",
			topology:  structs.MockWorkstationTopology(),
			available: []hw.CoreID{},
			cores:     1,
			expCores:  nil,
			expMHz:    0,
		},
		{
			name:     "ws: insufficient cores",
			topology: structs.MockWorkstationTopology(),
			available: []hw.CoreID{
				3, 5, 9, // node 0
				2, // node 1
			},
			cores:    5,
			expCores: nil,
			expMHz:   0,
		},
		{
			name:     "ws: node 0 smallest and fits",
			topology: structs.MockWorkstationTopology(),
			available: []hw.CoreID{
				1, 3, 5, 7, 9, // node 0
				2, 4, 6, // node 1
			},
			cores:    2,
			expCores: []uint16{2, 4},
			expMHz:   3_000 * 2,
		},
		{
			name:     "ws: node 1 smallest and fits",
			topology: structs.MockWorkstationTopology(),
			available: []hw.CoreID{
				9, 11, 13, // node 0
				2, 4, 6, 8, // node 1
			},
			cores:    2,
			expCores: []uint16{9, 11},
			expMHz:   3_000 * 2,
		},
		{
			name:     "ws: node 0 smallest and overflows",
			topology: structs.MockWorkstationTopology(),
			available: []hw.CoreID{
				1, 3, 5, 7, 9, // node 0
				2, 4, 6, // node 1
			},
			cores:    5,
			expCores: []uint16{2, 4, 6, 1, 3},
			expMHz:   3_000 * 5,
		},
		{
			name:     "ws: node 1 smallest and overflows",
			topology: structs.MockWorkstationTopology(),
			available: []hw.CoreID{
				1, 3, // node 0
				0, 2, 4, 6, 8, 10, // node 1
			},
			cores:    5,
			expCores: []uint16{1, 3, 0, 2, 4},
			expMHz:   3_000 * 5,
		},
		{
			name:      "r6: no cores available",
			topology:  structs.MockR6aTopology(),
			available: []hw.CoreID{},
			cores:     1,
			expCores:  nil,
			expMHz:    0,
		},
		{
			name:     "r6: node 0 is smallest and fits",
			topology: structs.MockR6aTopology(),
			available: []hw.CoreID{
				0, 20, 99, // node 0
				25, 27, 29, 31, // node1
				145, 146, 147, 148, // node 2
				72, 73, 168, 170, 190, 191, // node 3
			},
			cores:    2,
			expCores: []uint16{0, 20},
			expMHz:   3_731 * 2,
		},
		{
			name:     "r6: node 1 is smallest and fits",
			topology: structs.MockR6aTopology(),
			available: []hw.CoreID{
				0, 20, 99, 100, // node 0
				25, 27, 31, // node 1
				145, 146, 147, 148, // node 2
				72, 73, 168, 170, 190, 191, // node 3
			},
			cores:    3,
			expCores: []uint16{25, 27, 31},
			expMHz:   3_731 * 3,
		},
		{
			name:     "r6: node 2 is smallest and fits",
			topology: structs.MockR6aTopology(),
			available: []hw.CoreID{
				0, 20, 99, 100, // node 0
				25, 27, 31, 32, 33, // node 1
				145, 146, 148, // node 2
				72, 73, 168, 170, 190, 191, // node 3
			},
			cores:    1,
			expCores: []uint16{145},
			expMHz:   3_731 * 1,
		},
		{
			name:     "r6: node 3 is smallest and fits",
			topology: structs.MockR6aTopology(),
			available: []hw.CoreID{
				0, 20, 99, 100, // node 0
				25, 27, 31, 32, 33, // node 1
				145, 146, 148, // node 2
				190, 191, // node 3
			},
			cores:    1,
			expCores: []uint16{190},
			expMHz:   3_731 * 1,
		},
		{
			name:     "r6: fill node 1 and use node 3",
			topology: structs.MockR6aTopology(),
			available: []hw.CoreID{
				2, 3, 4, 5, 6, 7, // node 0
				25, 31, // node 1
				145, 146, 147, 148, 149, // node 2
				72, 168, 169, 170, // node 3
			},
			cores:    5,
			expCores: []uint16{25, 31, 72, 168, 169},
			expMHz:   3_731 * 5,
		},
		{
			name:     "r6: fill node 1, 0, 3 and use node 2",
			topology: structs.MockR6aTopology(),
			available: []hw.CoreID{
				2, 3, 4,
				25, 27,
				145, 146, 147, 148, 149,
				72, 73, 74, 75, 76,
			},
			cores:    7,
			expCores: []uint16{25, 27, 2, 3, 4, 145, 146},
			expMHz:   3_731 * 7,
		},
		{
			name:     "r6: fill all",
			topology: structs.MockR6aTopology(),
			available: []hw.CoreID{
				0,      // node 0
				25, 26, // node 1
				145, 146, 147, // node 2
				72, 73, 74, 75, // node 3
			},
			cores:    10,
			expCores: []uint16{0, 25, 26, 145, 146, 147, 72, 73, 74, 75},
			expMHz:   3_731 * 10,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := coreSelector{
				topology:       tc.topology,
				availableCores: idset.From[hw.CoreID](tc.available),
				shuffle:        orderCores,
			}

			ask := &structs.Resources{
				Cores: tc.cores,
				NUMA: &structs.NUMA{
					Affinity: structs.NoneNUMA,
				},
			}

			ids, mhz := cs.Select(ask)
			must.Eq(t, tc.expCores, ids)
			must.Eq(t, tc.expMHz, mhz)
		})
	}
}

func TestNUMA_Select_require(t *testing.T) {
	ci.Parallel(t)

	cases := []numaTestCase{
		{
			name:      "vm: no cores available",
			topology:  structs.MockBasicTopology(),
			available: []hw.CoreID{},
			cores:     1,
			expCores:  nil,
			expMHz:    0,
		},
		{
			name:      "vm: one core available",
			topology:  structs.MockBasicTopology(),
			available: []hw.CoreID{3},
			cores:     1,
			expCores:  []uint16{3},
			expMHz:    3_500,
		},
		{
			name:      "vm: insufficient cores available",
			topology:  structs.MockBasicTopology(),
			available: []hw.CoreID{0, 2, 3},
			cores:     4,
			expCores:  nil,
			expMHz:    0,
		},
		{
			name:      "vm: select two from four",
			topology:  structs.MockBasicTopology(),
			available: []hw.CoreID{0, 1, 2, 3},
			cores:     2,
			expCores:  []uint16{0, 1},
			expMHz:    3_500 * 2,
		},
		{
			name:     "ws: four cores available node 0",
			topology: structs.MockWorkstationTopology(),
			available: []hw.CoreID{
				3, 9, // node 0
				2, 6, 8, 10, // node 1
			},
			cores:    4,
			expCores: []uint16{2, 6, 8, 10},
			expMHz:   3_000 * 4,
		},
		{
			name:     "ws: four cores available node 1",
			topology: structs.MockWorkstationTopology(),
			available: []hw.CoreID{
				1, 3, 9, 11, // node 0
				2, 6, 8, // node 1
			},
			cores:    4,
			expCores: []uint16{1, 3, 9, 11},
			expMHz:   3_000 * 4,
		},
		{
			name:     "ws: four cores available but split on nodes",
			topology: structs.MockWorkstationTopology(),
			available: []hw.CoreID{
				1, 3, // node 0
				0, 2, // node 1
			},
			cores:    3,
			expCores: nil,
			expMHz:   0,
		},
		{
			// best fit dictates we choose the smallest available option
			name:     "ws: holes of size 4, 3",
			topology: structs.MockWorkstationTopology(),
			available: []hw.CoreID{
				3, 9, 11, // node 0
				2, 4, 6, 8, // node 1
			},
			cores:    2,
			expCores: []uint16{3, 9},
			expMHz:   3_000 * 2,
		},
		{
			name:      "r6: no cores available",
			topology:  structs.MockR6aTopology(),
			available: []hw.CoreID{},
			cores:     1,
			expCores:  nil,
			expMHz:    0,
		},
		{
			name:      "r6: no group large enough",
			topology:  structs.MockR6aTopology(),
			available: []hw.CoreID{10, 11, 12},
			cores:     4,
			expCores:  nil,
			expMHz:    0,
		},
		{
			name:     "r6: available across core split",
			topology: structs.MockR6aTopology(),
			available: []hw.CoreID{
				5, 6, 99, 100, 101, // node 0
				24, 25, 26, // node 1
				48, 49, 50, // node 2
				72, 73, 74, // node 3
			},
			cores:    4,
			expCores: []uint16{5, 6, 99, 100},
			expMHz:   3_731 * 4,
		},
		{
			name:     "r6: best fit on node 2",
			topology: structs.MockR6aTopology(),
			available: []hw.CoreID{
				1, 2, 7, 8, 9, 10, 11, 100, 101, // node 0 (9 open)
				30, 31, 32, 33, 34, 35, 36, 37, // node 1 (8 open)
				55, 56, 57, 150, 151, 152, // node 2 (6 open)
				168, 169, 170, 171, 172, 173, 174, // node 3 (7 open)
			},
			cores:    5,
			expCores: []uint16{55, 56, 57, 150, 151},
			expMHz:   3_731 * 5,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := coreSelector{
				topology:       tc.topology,
				availableCores: idset.From[hw.CoreID](tc.available),
				shuffle:        orderCores,
			}

			ask := &structs.Resources{
				Cores: tc.cores,
				NUMA: &structs.NUMA{
					Affinity: structs.RequireNUMA,
				},
			}

			ids, mhz := cs.Select(ask)
			must.Eq(t, tc.expCores, ids)
			must.Eq(t, tc.expMHz, mhz)
		})
	}
}

func TestNUMA_cost(t *testing.T) {
	ci.Parallel(t)

	cases := []struct {
		name     string
		topology *numalib.Topology
		cores    []int
		exp      int
	}{
		{
			name:     "vm: one",
			topology: structs.MockBasicTopology(),
			cores:    []int{0},
			exp:      0, // just itself
		},
		{
			name:     "vm: two",
			topology: structs.MockBasicTopology(),
			cores:    []int{1, 3},
			exp:      10, // (1,3)
		},
		{
			name:     "vm: three",
			topology: structs.MockBasicTopology(),
			cores:    []int{1, 2, 3},
			exp:      30, // (1, 2), (2, 3), (1, 3)
		},
		{
			name:     "vm: four",
			topology: structs.MockBasicTopology(),
			cores:    []int{0, 1, 2, 3},
			exp:      60, // 10 * 6 pairs
		},
		{
			name:     "ws: one",
			topology: structs.MockWorkstationTopology(),
			cores:    []int{31},
			exp:      0, // just itself
		},
		{
			name:     "ws: three on node 1",
			topology: structs.MockWorkstationTopology(),
			cores: []int{
				0, 4, 8, // node 1
			},
			exp: 30, // 10 * 3 pairs
		},
		{
			name:     "ws: two on node 0",
			topology: structs.MockWorkstationTopology(),
			cores: []int{
				5, 11, // node 0
			},
			exp: 10, // 1 pair
		},
		{
			name:     "ws: split one each",
			topology: structs.MockWorkstationTopology(),
			cores:    []int{0, 1},
			exp:      20, // 1 split pair
		},
		{
			name:     "ws: split two each",
			topology: structs.MockWorkstationTopology(),
			cores:    []int{1, 4, 5, 8},
			// (1, 4) - 20 (split)
			// (1, 5) - 10 (same)
			// (1, 8) - 20 (split)
			// (4, 5) - 20 (split)
			// (4, 8) - 10 (same)
			// (5, 8) - 20 (split)
			exp: 100,
		},
		{
			name:     "ws: one and two",
			topology: structs.MockWorkstationTopology(),
			cores:    []int{3, 8, 31},
			// (3, 8)  - 20 (split)
			// (3, 31) - 10 (same)
			// (8, 31) - 20 (split)
			exp: 50,
		},
		{
			name:     "r6: one",
			topology: structs.MockR6aTopology(),
			cores:    []int{1},
			exp:      0,
		},
		{
			name:     "r6: one on each node",
			topology: structs.MockR6aTopology(),
			cores:    []int{0, 24, 48, 72},
			// (00, 24) - 12
			// (00, 48) - 32
			// (00, 72) - 32
			// (24, 48) - 32
			// (24, 72) - 32
			// (48, 72) - 12
			exp: 152,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := &coreSelector{
				topology: tc.topology,
			}

			// build up the slice of actual cores from the topology
			// so we can inquire about distance data given the node
			// of each core
			cores := make([]numalib.Core, 0, len(tc.cores))
			for _, core := range tc.topology.Cores {
				id := int(core.ID)
				if slices.Contains(tc.cores, id) {
					cores = append(cores, core)
				}
			}

			result := cs.cost(cores)
			must.Eq(t, tc.exp, result)
		})
	}
}

func TestNUMA_Select_prefer(t *testing.T) {
	ci.Parallel(t)

	cases := []numaTestCase{
		{
			name:      "vm: no cores available",
			topology:  structs.MockBasicTopology(),
			available: []hw.CoreID{},
			cores:     1,
			expCores:  nil,
			expMHz:    0,
		},
		{
			name:      "vm: one core available",
			topology:  structs.MockBasicTopology(),
			available: []hw.CoreID{2},
			cores:     1,
			expCores:  []uint16{2},
			expMHz:    3_500 * 1,
		},
		{
			name:      "vm: insufficient cores",
			topology:  structs.MockBasicTopology(),
			available: []hw.CoreID{0, 1},
			cores:     3,
			expCores:  nil,
			expMHz:    0,
		},
		{
			name:      "vm: use two of three",
			topology:  structs.MockBasicTopology(),
			available: []hw.CoreID{0, 2, 3},
			cores:     2,
			expCores:  []uint16{0, 2},
			expMHz:    3_500 * 2,
		},
		{
			name:      "vm: use all",
			topology:  structs.MockBasicTopology(),
			available: []hw.CoreID{0, 1, 2, 3},
			cores:     4,
			expCores:  []uint16{0, 1, 2, 3},
			expMHz:    3_500 * 4,
		},
		{
			name:      "ws: no cores available",
			topology:  structs.MockWorkstationTopology(),
			available: []hw.CoreID{},
			cores:     1,
			expCores:  nil,
			expMHz:    0,
		},
		{
			name:      "ws: one core available",
			topology:  structs.MockWorkstationTopology(),
			available: []hw.CoreID{3},
			cores:     1,
			expCores:  []uint16{3},
			expMHz:    3_000 * 1,
		},
		{
			name:      "ws: insufficient cores available",
			topology:  structs.MockWorkstationTopology(),
			available: []hw.CoreID{1, 2, 3, 4},
			cores:     5,
			expCores:  nil,
			expMHz:    0,
		},
		{
			name:      "ws: fits in node 0",
			topology:  structs.MockWorkstationTopology(),
			available: []hw.CoreID{1, 2, 3, 5, 6, 7},
			cores:     4,
			expCores:  []uint16{1, 3, 5, 7},
			expMHz:    3_000 * 4,
		},
		{
			name:      "ws: fits in node 1",
			topology:  structs.MockWorkstationTopology(),
			available: []hw.CoreID{1, 2, 3, 4, 8, 10},
			cores:     4,
			expCores:  []uint16{2, 4, 8, 10},
			expMHz:    3_000 * 4,
		},
		{
			name:      "ws: split and fill",
			topology:  structs.MockWorkstationTopology(),
			available: []hw.CoreID{1, 2, 3, 7},
			cores:     4,
			expCores:  []uint16{1, 2, 3, 7},
			expMHz:    3_000 * 4,
		},
		{
			name:      "ws: fill node 0 and overflow",
			topology:  structs.MockWorkstationTopology(),
			available: []hw.CoreID{0, 1, 2, 3},
			cores:     3,
			expCores:  []uint16{0, 1, 2},
			expMHz:    3_000 * 3,
		},
		{
			name:      "ws: fill node 1 and overflow",
			topology:  structs.MockWorkstationTopology(),
			available: []hw.CoreID{1, 2, 4, 7, 9},
			cores:     4,
			expCores:  []uint16{1, 2, 7, 9},
			expMHz:    3_000 * 4,
		},
		{
			name:      "r6: no cores available",
			topology:  structs.MockR6aTopology(),
			available: []hw.CoreID{},
			cores:     1,
			expCores:  nil,
			expMHz:    0,
		},
		{
			name:      "r6: one core available",
			topology:  structs.MockR6aTopology(),
			available: []hw.CoreID{17},
			cores:     1,
			expCores:  []uint16{17},
			expMHz:    3_731 * 1,
		},
		{
			name:      "r6: insufficient cores available",
			topology:  structs.MockR6aTopology(),
			available: []hw.CoreID{1, 10, 100},
			cores:     4,
			expCores:  nil,
			expMHz:    0,
		},
		{
			name:     "r6: fits on node 0",
			topology: structs.MockR6aTopology(),
			available: []hw.CoreID{
				1, 3, 99, 100, 101, // node 0
				24, 120, // node 1
				50, 51, 52, // node 2
				72, 73, 169, // node 3
			},
			cores:    4,
			expCores: []uint16{1, 3, 99, 100},
			expMHz:   3_731 * 4,
		},
		{
			name:     "r6: fits on node 1",
			topology: structs.MockR6aTopology(),
			available: []hw.CoreID{
				1, 3, 99, 101, // node 0
				24, 26, 120, 121, 122, // node 1
				50, 51, 52, // node 2
				72, 73, 169, // node 3
			},
			cores:    5,
			expCores: []uint16{24, 26, 120, 121, 122},
			expMHz:   3_731 * 5,
		},
		{
			name:     "r6: fits on node 2",
			topology: structs.MockR6aTopology(),
			available: []hw.CoreID{
				1, 3, 99, 101, // node 0
				24, 26, 120, 122, // node 1
				50, 51, 52, 53, 54, 55, // node 2
				72, 73, 169, // node 3
			},
			cores:    6,
			expCores: []uint16{50, 51, 52, 53, 54, 55},
			expMHz:   3_731 * 6,
		},
		{
			name:     "r6: fits on node 3",
			topology: structs.MockR6aTopology(),
			available: []hw.CoreID{
				1, 3, 99, 101, // node 0
				24, 26, 120, 122, // node 1
				50, 51, 52, 53, 54, // node 2
				72, 73, 74, 169, 170, 177, 179, // node 3
			},
			cores:    6,
			expCores: []uint16{72, 73, 74, 169, 170, 177},
			expMHz:   3_731 * 6,
		},
		{
			name:     "r6: node 0 overflow to node 1",
			topology: structs.MockR6aTopology(),
			available: []hw.CoreID{
				10, 11, // node 0 (socket 0)
				24, 25, // node 1 (socket 0)
				50, // node 2 (socket 1)
				72, // node 3 (socket 2)
			},
			cores:    4,
			expCores: []uint16{10, 11, 24, 25},
			expMHz:   4 * 3_731,
		},
		{
			name:     "r6: node 2 overflow node 1",
			topology: structs.MockR6aTopology(),
			available: []hw.CoreID{
				24, 25, // node 1 (socket 0)
				50, 51, 52, // node 2 (socket 1)
			},
			cores:    4,
			expCores: []uint16{24, 50, 51, 52},
			expMHz:   4 * 3_731,
		},
		{
			name:     "r6: node 0 overflow node 1 and node 2",
			topology: structs.MockR6aTopology(),
			available: []hw.CoreID{
				1, 101, 102, // node 0 (socket 0)
				28,     // node 1 (socket 0)
				55, 56, // node 2 (socket 1)
				77, // node 3 (socket 1)
			},
			cores:    5,
			expCores: []uint16{1, 28, 55, 101, 102},
			expMHz:   5 * 3_731,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := coreSelector{
				topology:       tc.topology,
				availableCores: idset.From[hw.CoreID](tc.available),
				shuffle:        orderCores,
			}

			ask := &structs.Resources{
				Cores: tc.cores,
				NUMA: &structs.NUMA{
					Affinity: structs.PreferNUMA,
				},
			}

			ids, mhz := cs.Select(ask)
			must.SliceContainsAll(t, tc.expCores, ids)
			must.Eq(t, tc.expMHz, mhz)
		})
	}
}
