package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	LiveHandoffProtocolV2         = 2
	LiveHandoffCreationIncomplete = "creation_incomplete"
	LiveHandoffFreezeIncomplete   = "freeze_incomplete"
	// Guard-only can be a partial creation or an expired call; PG state decides.
	LiveHandoffGuardOnly = "guard_only"
)

var (
	ErrLiveHandoffProtocol    = errors.New("live handoff protocol mismatch")
	ErrLiveHandoffIncomplete  = errors.New("live handoff is incomplete")
	ErrLiveHandoffUnconfirmed = errors.New("live handoff creation is not confirmed")
)

// LiveHandoffBoundReservation contains only the PG-bound facts needed for creation.
type LiveHandoffBoundReservation struct {
	OperationID         string
	CallHash            string
	APIKeyID            int64
	IdentityFingerprint string
	Generation          int64
	Status              string
}

type LiveHandoffBoundLookup interface {
	LookupBoundLiveHandoff(ctx context.Context, callHash string, apiKeyID int64) (LiveHandoffBoundReservation, error)
}

type LiveHandoffCreationResult struct {
	OperationID         string
	CallHash            string
	APIKeyID            int64
	IdentityFingerprint string
	Generation          int64
	ProtocolVersion     int
}

type LiveHandoffCreationStore interface {
	CreateLiveCallV2(ctx context.Context, record *LiveCallRecord, bound LiveHandoffBoundReservation, ttl time.Duration) (LiveHandoffCreationResult, error)
}

// ConfirmedLiveHandoffCreation prevents accidental use of an unchecked creation.
// Its private payload is an in-process guard, not an authentication boundary.
type ConfirmedLiveHandoffCreation struct{ payload string }

func (proof ConfirmedLiveHandoffCreation) Validate() (LiveHandoffCreationResult, error) {
	var result LiveHandoffCreationResult
	if proof.payload == "" || json.Unmarshal([]byte(proof.payload), &result) != nil ||
		strings.TrimSpace(result.OperationID) == "" || strings.TrimSpace(result.CallHash) == "" ||
		result.APIKeyID <= 0 || strings.TrimSpace(result.IdentityFingerprint) == "" ||
		result.Generation <= 0 || result.ProtocolVersion != LiveHandoffProtocolV2 {
		return LiveHandoffCreationResult{}, ErrLiveHandoffUnconfirmed
	}
	return result, nil
}

// ConfirmLiveHandoffCreation checks a committed bound reservation and requires
// the Redis creation operation to confirm both the record and its guard.
func ConfirmLiveHandoffCreation(ctx context.Context, lookup LiveHandoffBoundLookup, redis LiveHandoffCreationStore, record *LiveCallRecord, expectedGeneration int64, ttl time.Duration) (ConfirmedLiveHandoffCreation, error) {
	if lookup == nil || redis == nil || record == nil || expectedGeneration <= 0 || ttl < time.Millisecond {
		return ConfirmedLiveHandoffCreation{}, ErrLiveHandoffUnconfirmed
	}
	copyRecord := *record
	if record.RealtimePricePerMin != nil {
		price := *record.RealtimePricePerMin
		copyRecord.RealtimePricePerMin = &price
	}
	if record.BillingPolicy != nil {
		policy := *record.BillingPolicy
		copyRecord.BillingPolicy = &policy
	}
	if record.Settlement != nil {
		settlement := *record.Settlement
		copyRecord.Settlement = &settlement
	}
	_, fingerprint, err := LiveSettlementIdentityFromRecord(&copyRecord)
	if err != nil {
		return ConfirmedLiveHandoffCreation{}, err
	}
	bound, err := lookup.LookupBoundLiveHandoff(ctx, copyRecord.CallHash, copyRecord.APIKeyID)
	if err != nil {
		return ConfirmedLiveHandoffCreation{}, err
	}
	if bound.Status != "bound" || strings.TrimSpace(bound.OperationID) == "" || bound.CallHash != copyRecord.CallHash ||
		bound.APIKeyID != copyRecord.APIKeyID || bound.IdentityFingerprint != fingerprint || bound.Generation != expectedGeneration {
		return ConfirmedLiveHandoffCreation{}, ErrLiveHandoffUnconfirmed
	}
	result, err := redis.CreateLiveCallV2(ctx, &copyRecord, bound, ttl)
	if err != nil {
		return ConfirmedLiveHandoffCreation{}, err
	}
	expected := LiveHandoffCreationResult{OperationID: bound.OperationID, CallHash: bound.CallHash,
		APIKeyID: bound.APIKeyID, IdentityFingerprint: bound.IdentityFingerprint, Generation: bound.Generation,
		ProtocolVersion: LiveHandoffProtocolV2}
	if result != expected {
		return ConfirmedLiveHandoffCreation{}, ErrLiveHandoffUnconfirmed
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return ConfirmedLiveHandoffCreation{}, err
	}
	return ConfirmedLiveHandoffCreation{payload: string(encoded)}, nil
}

// LiveHandoffAckReference must be supplied only after a durable PG authorization.
// Redis can compare these facts, but cannot independently verify that authorization.
type LiveHandoffAckReference struct {
	CallHash            string
	IdentityFingerprint string
	FrozenDigest        string
	Generation          int64
}

type LiveHandoffStatus struct {
	Phase               string
	IdentityFingerprint string
	FrozenDigest        string
	Generation          int64
}
