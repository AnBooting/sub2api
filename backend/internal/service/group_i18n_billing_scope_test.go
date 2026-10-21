package service

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGatewayGroupSnapshotKeepsOriginalNameAndCarriesNoI18n 锁住分组 i18n 的第二条语义：
// 网关侧（鉴权缓存快照 / HA 候选）拿到的分组名恒为原名，且快照里没有译文表字段——
// 网关无从本地化，计费口径与请求语言无关。
//
// 我们的鉴权缓存快照是显式逐字段拷贝（版本高于上游），因此不需要为 i18n 提版本；
// 一旦有人往快照里加 I18n 字段，就是改了缓存格式，必须显式提版本而不是默默带上译文表。
func TestGatewayGroupSnapshotKeepsOriginalNameAndCarriesNoI18n(t *testing.T) {
	source := &Group{
		ID: 9, Name: "标准", Description: "号池", Platform: "anthropic",
		I18n: GroupI18n{"en": {Name: "Standard", Description: "Pro + Max pool"}},
	}

	snapshot := apiKeyAuthSnapshotFromGroup(source)
	require.NotNil(t, snapshot)
	require.Equal(t, "标准", snapshot.Name, "鉴权缓存快照必须保留分组原名")
	require.Equal(t, "标准", source.Name, "快照构造不得改写源分组")

	_, snapshotHasI18n := reflect.TypeOf(*snapshot).FieldByName("I18n")
	require.False(t, snapshotHasI18n, "鉴权缓存快照不得携带 i18n 译文表")

	_, haHasI18n := reflect.TypeOf(HAGroupCandidate{}).FieldByName("I18n")
	require.False(t, haHasI18n, "HA 候选快照不得携带 i18n 译文表")
}
