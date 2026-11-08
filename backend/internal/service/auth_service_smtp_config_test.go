//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Embedding the interface makes unexpected cache operations fail the test.
type smtpConfigVerifyCacheSpy struct {
	EmailCache
	reads  int
	writes int
}

func (s *smtpConfigVerifyCacheSpy) GetVerificationCode(context.Context, string) (*VerificationCodeData, error) {
	s.reads++
	// Stop the pre-fix synchronous path before it can attempt a real SMTP dial.
	return &VerificationCodeData{CreatedAt: time.Now()}, nil
}

func (s *smtpConfigVerifyCacheSpy) SetVerificationCode(context.Context, string, *VerificationCodeData, time.Duration) error {
	s.writes++
	return nil
}

func TestAuthService_SendVerifyCode_SMTPConfiguration(t *testing.T) {
	for _, async := range []bool{false, true} {
		mode := "sync"
		if async {
			mode = "async"
		}
		for _, state := range []string{"missing_host", "blank_host", "missing_from", "blank_from", "missing_service", "settings_error"} {
			t.Run(mode+"/"+state, func(t *testing.T) {
				settings := map[string]string{SettingKeyRegistrationEnabled: "true"}
				if state == "blank_host" {
					settings[SettingKeySMTPHost] = " \t "
				}
				if state == "missing_from" || state == "blank_from" {
					settings[SettingKeySMTPHost] = "smtp.example.com"
					settings[SettingKeySMTPFrom] = " \t "
					if state == "missing_from" {
						delete(settings, SettingKeySMTPFrom)
					}
				}
				cache := &smtpConfigVerifyCacheSpy{}
				svc := newAuthService(&userRepoStub{}, settings, cache, nil)
				queue := &EmailQueueService{taskChan: make(chan EmailTask, 1)}
				svc.emailQueueService = queue
				switch state {
				case "missing_service":
					svc.emailService = nil
				case "settings_error":
					svc.emailService.settingRepo = &settingRepoStub{err: errors.New("smtp settings unavailable")}
				}

				var err error
				if async {
					var result *SendVerifyCodeResult
					result, err = svc.SendVerifyCodeAsync(context.Background(), "user@example.com")
					require.Nil(t, result)
				} else {
					err = svc.SendVerifyCode(context.Background(), "user@example.com")
				}

				require.ErrorIs(t, err, ErrEmailNotConfigured)
				require.Empty(t, queue.taskChan, "unconfigured SMTP must not enqueue mail")
				require.Zero(t, cache.reads, "reject before consuming code cooldown")
				require.Zero(t, cache.writes, "reject before storing a verification code")
			})
		}
	}
}

func TestAuthService_SendVerifyCode_ConfiguredSMTPUsesExistingCooldown(t *testing.T) {
	settings := map[string]string{
		SettingKeyRegistrationEnabled: "true",
		SettingKeySMTPHost:            "smtp.example.com",
		SettingKeySMTPFrom:            "sender@example.com",
	}
	cache := &smtpConfigVerifyCacheSpy{}
	svc := newAuthService(&userRepoStub{}, settings, cache, nil)

	err := svc.SendVerifyCode(context.Background(), "user@example.com")

	require.ErrorIs(t, err, ErrVerifyCodeTooFrequent)
	require.Equal(t, 1, cache.reads)
	require.Zero(t, cache.writes)
}

func TestAuthService_SendVerifyCodeAsync_ConfiguredSMTPEnqueues(t *testing.T) {
	settings := map[string]string{
		SettingKeyRegistrationEnabled: "true",
		SettingKeySMTPHost:            "smtp.example.com",
		SettingKeySMTPFrom:            "sender@example.com",
		SettingKeySiteName:            "Test Site",
	}
	cache := &smtpConfigVerifyCacheSpy{}
	svc := newAuthService(&userRepoStub{}, settings, cache, nil)
	queue := &EmailQueueService{taskChan: make(chan EmailTask, 1)}
	svc.emailQueueService = queue

	result, err := svc.SendVerifyCodeAsync(context.Background(), "user@example.com", "zh-CN")

	require.NoError(t, err)
	require.Equal(t, 60, result.Countdown)
	require.Len(t, queue.taskChan, 1)
	require.Equal(t, EmailTask{
		Email: "user@example.com", SiteName: "Test Site", TaskType: TaskTypeVerifyCode, Locale: "zh-CN",
	}, <-queue.taskChan)
	require.Zero(t, cache.reads)
	require.Zero(t, cache.writes)
}
