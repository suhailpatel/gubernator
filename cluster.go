/*
Copyright 2018-2022 Mailgun Technologies Inc

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gubernator

import (
	"github.com/mailgun/holster/v4/syncutil"
	"github.com/sirupsen/logrus"
)

type remoteClusterManager struct {
	reqQueue chan *RateLimitReq
	wg       syncutil.WaitGroup
	conf     BehaviorConfig
	log      logrus.FieldLogger
	instance *V1Instance
}

func newRemoteClusterManager(conf BehaviorConfig, instance *V1Instance) *remoteClusterManager {
	mm := remoteClusterManager{
		conf:     conf,
		instance: instance,
		log:      instance.log,
		reqQueue: make(chan *RateLimitReq, conf.MultiClusterBatchLimit),
	}
	mm.runAsyncReqs()
	return &mm
}

// QueueHits writes the RateLimitReq to be asynchronously sent to other clusters
func (mm *remoteClusterManager) QueueHits(r *RateLimitReq) {
	mm.reqQueue <- r
}

func (mm *remoteClusterManager) runAsyncReqs() {
	var interval = NewInterval(mm.conf.MultiClusterSyncWait)
	hits := make(map[string]*RateLimitReq)

	mm.wg.Until(func(done chan struct{}) bool {
		select {
		case r := <-mm.reqQueue:
			key := r.HashKey()

			// Aggregate the hits into a single request
			_, ok := hits[key]
			if ok {
				hits[key].Hits += r.Hits
			} else {
				hits[key] = r
			}

			// Send the hits if we reached our batch limit
			if len(hits) == mm.conf.MultiClusterBatchLimit {
				for dc, picker := range mm.instance.GetRegionPickers() {
					mm.log.Debugf("Sending %v hit(s) to %s picker", len(hits), dc)
					mm.sendHits(hits, picker)
				}
				hits = make(map[string]*RateLimitReq)
			}

			// Queue next interval
			if len(hits) == 1 {
				interval.Next()
			}

		case <-interval.C:
			if len(hits) > 0 {
				for dc, picker := range mm.instance.GetRegionPickers() {
					mm.log.Debugf("Sending %v hit(s) to %s picker", len(hits), dc)
					mm.sendHits(hits, picker)
				}
				hits = make(map[string]*RateLimitReq)
			}

		case <-done:
			return false
		}
		return true
	})
}

// TODO: Sending cross DC should mainly update the hits, the config should not be sent, or ignored when received
// TODO: Calculation of OVERLIMIT should not occur when sending hits cross DC
func (mm *remoteClusterManager) sendHits(r map[string]*RateLimitReq, picker PeerPicker) {
	// Does nothing for now
}

func (mm *remoteClusterManager) Close() {
	mm.wg.Stop()
}
