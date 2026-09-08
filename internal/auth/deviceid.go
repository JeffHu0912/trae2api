// deviceid.go — 保证每个账号有一个可用于每日签到的 deviceId。
//
// 背景：TRAE/TraeWork 签到与积分查询(ug)接口的 X-Device-Id 需要是「每个账号互不相同」的值，
// 同一天若两个账号共用同一 deviceId，第二个账号会被"该设备已签到"拦截；空 deviceId 则签到报 9004。
// 真实设备号(ahaDeviceService 注册)是 16 位纯数字；本机实测任意互异的 16 位数字串也能签到成功。
//
// 因此：新增账号落盘前，若 DeviceID 为空则自动填一个随机 16 位数字串并固化进 auth 文件，
// 保证该账号始终用同一 deviceId 签到、且与其他账号互不相同。

package auth

import (
	"crypto/rand"
	"math/big"
)

// NewCheckinDeviceID 生成一个 16 位纯数字的签到 deviceId（首字符非 0）。
// 采用 crypto/rand 保证无预测性；格式与真实注册设备号一致(16 位数字)。
func NewCheckinDeviceID() (string, error) {
	// 首字符 1-9，其余 0-9，共 16 位
	digits := make([]byte, 16)
	first, err := rand.Int(rand.Reader, big.NewInt(9))
	if err != nil {
		return "", err
	}
	digits[0] = byte('1' + first.Int64())
	for i := 1; i < 16; i++ {
		d, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		digits[i] = byte('0' + d.Int64())
	}
	return string(digits), nil
}

// EnsureCheckinDeviceID 在账号首次落盘前调用：仅当 DeviceID 为空时才自动生成并写入。
// 已有的(用户导入/真实注册的) deviceId 一律保留，不覆盖。
// 返回是否发生了生成(true)或错误。
func (a *Auth) EnsureCheckinDeviceID() (bool, error) {
	if a.DeviceID != "" {
		return false, nil
	}
	id, err := NewCheckinDeviceID()
	if err != nil {
		return false, err
	}
	a.DeviceID = id
	return true, nil
}
