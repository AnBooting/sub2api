//go:build integration

package repository

import "github.com/Wei-Shaw/sub2api/internal/service"

// Uses the existing PostgreSQL testEntTx harness; unavailable DB is an explicit skip.
func (s *APIKeyRepoSuite) TestGroupChainCASAndConcurrentUsage() {
	user := s.mustCreateUser("chain-cas-integration@test.com")
	a := s.mustCreateGroup("chain-cas-a")
	b := s.mustCreateGroup("chain-cas-b")
	key := &service.APIKey{UserID: user.ID, Key: "sk-chain-cas-integration", Name: "before", Status: service.StatusActive, Quota: 100, Groups: []service.APIKeyGroupLink{{GroupID: a.ID, Role: "normal", Position: 0}, {GroupID: b.ID, Role: "normal", Position: 1}}}
	s.Require().NoError(s.repo.Create(s.ctx, key))
	stale, err := s.repo.GetByID(s.ctx, key.ID)
	s.Require().NoError(err)
	_, err = s.repo.IncrementQuotaUsed(s.ctx, key.ID, 30)
	s.Require().NoError(err)
	revision := stale.GroupChainRevision
	key.Groups = []service.APIKeyGroupLink{{GroupID: b.ID, Role: "normal", Position: 0}, {GroupID: a.ID, Role: "normal", Position: 1}}
	s.Require().NoError(s.repo.Update(s.ctx, key, service.APIKeyUpdateFields{Groups: true, ExpectedGroupChainRevision: &revision}))
	stale.Name = "stale-must-not-write"
	stale.Groups = []service.APIKeyGroupLink{}
	s.Require().ErrorIs(s.repo.Update(s.ctx, stale, service.APIKeyUpdateFields{Name: true, Groups: true, ExpectedGroupChainRevision: &revision}), service.ErrGroupChainConflict)
	got, err := s.repo.GetByID(s.ctx, key.ID)
	s.Require().NoError(err)
	s.Require().Equal("before", got.Name)
	s.Require().Equal(b.ID, *got.GroupID)
	s.Require().Equal(int64(1), got.GroupChainRevision)
	s.Require().InDelta(30, got.QuotaUsed, 1e-9)
	s.Require().Len(got.Groups, 2)
}
