// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	containerapi "github.com/docker/docker/api/types/container"
	"github.com/hashicorp/nomad/client/lib/cpustats"
	cstructs "github.com/hashicorp/nomad/client/structs"
	"github.com/hashicorp/nomad/drivers/docker/util"
	"github.com/hashicorp/nomad/helper"
	nstructs "github.com/hashicorp/nomad/nomad/structs"
)

const (
	// statsCollectorBackoffBaseline is the baseline time for exponential
	// backoff while calling the docker stats api.
	statsCollectorBackoffBaseline = 5 * time.Second

	// statsCollectorBackoffLimit is the limit of the exponential backoff for
	// calling the docker stats api.
	statsCollectorBackoffLimit = 2 * time.Minute
)

// usageSender wraps a TaskResourceUsage chan such that it supports concurrent
// sending and closing, and backpressures by dropping events if necessary.
type usageSender struct {
	closed bool
	destCh chan<- *cstructs.TaskResourceUsage
	mu     sync.Mutex
}

// newStatsChanPipe returns a chan wrapped in a struct that supports concurrent
// sending and closing, and the receiver end of the chan.
func newStatsChanPipe() (*usageSender, <-chan *cstructs.TaskResourceUsage) {
	destCh := make(chan *cstructs.TaskResourceUsage, 1)
	return &usageSender{destCh: destCh}, destCh

}

// send resource usage to the receiver unless the chan is already full or
// closed.
func (u *usageSender) send(tru *cstructs.TaskResourceUsage) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.closed {
		return
	}

	select {
	case u.destCh <- tru:
	default:
		// Backpressure caused missed interval
	}
}

// close resource usage. Any further sends will be dropped.
func (u *usageSender) close() {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.closed {
		// already closed
		return
	}

	u.closed = true
	close(u.destCh)
}

// Stats starts collecting stats from the docker daemon and sends them on the
// returned channel.
func (h *taskHandle) Stats(ctx context.Context, interval time.Duration, compute cpustats.Compute) (<-chan *cstructs.TaskResourceUsage, error) {
	select {
	case <-h.doneCh:
		return nil, nstructs.NewRecoverableError(fmt.Errorf("container stopped"), false)
	default:
	}

	destCh, recvCh := newStatsChanPipe()
	go h.collectStats(ctx, destCh, interval, compute)
	return recvCh, nil
}

// collectStats starts collecting resource usage stats of a docker container
func (h *taskHandle) collectStats(ctx context.Context, destCh *usageSender, interval time.Duration, compute cpustats.Compute) {
	defer destCh.close()

	timer, cancel := helper.NewSafeTimer(interval)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.doneCh:
			return
		case <-timer.C:
			// ContainerStats returns a StatsResponseReader. Body of that reader
			// contains the stats and implements io.Reader
			statsReader, err := h.dockerClient.ContainerStatsOneShot(ctx, h.containerID)
			if err != nil && err != io.EOF {
				// An error occurred during stats collection, retry with backoff
				h.logger.Debug("error collecting stats from container", "error", err)
				continue
			}

			var stats containerapi.Stats

			if err := json.NewDecoder(statsReader.Body).Decode(&stats); err != nil {
				h.logger.Error("error unmarshalling stats data for container", "error", err)
				_ = statsReader.Body.Close()
				continue
			}

			resourceUsage := util.DockerStatsToTaskResourceUsage(&stats, compute)
			destCh.send(resourceUsage)

			_ = statsReader.Body.Close()
		}
	}
}
