package cluster

import (
	"context"

	"github.com/etouraille/queel"
)

// DistributedStore adapts a Coordinator to queel.Store, so a queel.Repository
// can run over a replicated, quorum-consistent cluster exactly as it would
// over a single local Engine — same interface, same domain logic, nothing
// else has to change.
type DistributedStore struct {
	coordinator *Coordinator
}

func NewDistributedStore(coordinator *Coordinator) *DistributedStore {
	return &DistributedStore{coordinator: coordinator}
}

func (s *DistributedStore) Put(key, value []byte) error {
	return s.coordinator.Put(context.Background(), string(key), value)
}

func (s *DistributedStore) Delete(key []byte) error {
	return s.coordinator.Delete(context.Background(), string(key))
}

func (s *DistributedStore) Get(key []byte) ([]byte, bool, error) {
	return s.coordinator.Get(context.Background(), string(key))
}

func (s *DistributedStore) Scan(prefix []byte) ([]queel.KV, error) {
	return s.coordinator.Scan(context.Background(), string(prefix))
}

func (s *DistributedStore) WriteBatch(ops []queel.WriteOp) error {
	return s.coordinator.WriteBatch(context.Background(), ops)
}
