//go:build unit

package service

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// F-03：sub2api.local 是**本平台自用的运维保留域**（setup 自动生成的初始管理员
// 就是 admin-<hex>@sub2api.local，见 internal/setup/setup.go generateAdminEmail）。
// 攻击者抢注 admin@sub2api.local 一类地址即可得到与管理员几乎同形的账号，
// 并让"发码/注册"链路向一个并非真实用户的地址承诺可投递。
//
// 因此整域保留：任何 <local>@sub2api.local 都不得通过 isReservedEmail。
// 这些断言直接调用生产函数，不在测试里复制域名判断逻辑。
func TestIsReservedEmail_Sub2APIInternalDomain(t *testing.T) {
	for _, email := range []string{
		"admin@sub2api.local",
		"admin@Sub2API.Local",
		"ADMIN@SUB2API.LOCAL",
		"  admin@sub2api.local  ", // 前后空格必须归一化后再判定
		"anything@sub2api.local",
		"a.b+tag@sub2api.local",
		"admin-39bb646744@sub2api.local", // setup 生成的真实管理员形态
	} {
		require.Truef(t, isReservedEmail(email), "sub2api.local 整域保留，%q 必须被拒绝", email)
	}
}

// 保留域的判定不得外溢到"看起来像"的正常域名，否则会误伤真实用户。
func TestIsReservedEmail_Sub2APILookalikesAreAllowed(t *testing.T) {
	for _, email := range []string{
		"real@gmail.com",
		"someone@sub2api.local.example.com", // 只是前缀相同
		"someone@notsub2api.local",          // 末段相同但域名不同
		"someone@sub2api.com",
		"someone@sub2api.localhost",
	} {
		require.Falsef(t, isReservedEmail(email), "%q 不是保留域，不得误判", email)
	}
}

// 注册链路必须把保留域映射成 EMAIL_RESERVED，而不是 EMAIL_EXISTS 或 500，
// 且必须在写库之前拒绝：断言 userRepo.Create 一次都没被调用。
func TestAuthService_Register_RejectsSub2APIInternalDomain(t *testing.T) {
	repo := &userRepoStub{nextID: 91}
	svc := newAuthService(repo, map[string]string{
		SettingKeyRegistrationEnabled: "true",
	}, nil, nil)

	_, user, err := svc.Register(context.Background(), "admin@sub2api.local", "password123")
	require.ErrorIs(t, err, ErrEmailReserved)
	require.Nil(t, user)
	require.Equal(t, "EMAIL_RESERVED", infraerrors.Reason(err))
	require.Empty(t, repo.created, "保留域必须在创建用户之前被拦截")
}

// 发码链路同样必须在入队前拦截，避免向保留域承诺验证码投递。
func TestAuthService_SendVerifyCode_RejectsSub2APIInternalDomain(t *testing.T) {
	svc := newAuthService(&userRepoStub{}, map[string]string{
		SettingKeyRegistrationEnabled: "true",
	}, nil, nil)

	err := svc.SendVerifyCode(context.Background(), "admin@sub2api.local")
	require.ErrorIs(t, err, ErrEmailReserved)
	require.Equal(t, "EMAIL_RESERVED", infraerrors.Reason(err))
}
