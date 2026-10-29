package store_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/store"
)

// 行 114：提醒偏好——默认全开 24h，可更新，非法提前量拒绝。
func TestNotificationPreferencesMySQL(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	repo := store.New(f.db)
	patientID := f.patient().Patient.ID

	// 默认
	pref, err := repo.NotificationPreferences(ctx, patientID)
	if err != nil {
		t.Fatal(err)
	}
	if !pref.AppointmentReminder || !pref.FollowupReminder || !pref.CareReminder || pref.LeadHours != 24 {
		t.Fatalf("default: %+v", pref)
	}
	// 更新
	updated, err := repo.UpdateNotificationPreferences(ctx, patientID, false, true, false, 48, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if updated.AppointmentReminder || !updated.FollowupReminder || updated.CareReminder || updated.LeadHours != 48 {
		t.Fatalf("updated: %+v", updated)
	}
	// 读回
	back, _ := repo.NotificationPreferences(ctx, patientID)
	if back.LeadHours != 48 || back.AppointmentReminder {
		t.Fatalf("persisted: %+v", back)
	}
	// 非法提前量
	if _, err := repo.UpdateNotificationPreferences(ctx, patientID, true, true, true, 999, f.now); err == nil {
		t.Fatal("expected error for leadHours > 168")
	}

	f.db.Exec("DELETE FROM notification_preferences WHERE patient_id = ?", patientID)
}

// 行 115：双因素验证——setup 生成密钥，验证码校验，启用/禁用。
func TestEmployee2FAMySQL(t *testing.T) {
	f := newBookingFixture(t)
	ctx := context.Background()
	repo := store.New(f.db)

	// 创建测试员工账号
	empID := "emp-2fa-test"
	f.db.Exec("INSERT INTO employee_accounts (id, username, display_name, password_hash, role, clinic_ids, active, version, created_at, updated_at) VALUES (?, 'tfa-test', '2FA测试', 'x', 'manager', '[\"c\"]', 1, 1, ?, ?)", empID, f.now, f.now)
	defer f.db.Exec("DELETE FROM employee_2fa WHERE employee_id = ?", empID)
	defer f.db.Exec("DELETE FROM employee_accounts WHERE id = ?", empID)

	// 未启用
	enabled, err := repo.Employee2FAEnabled(ctx, empID)
	if err != nil || enabled {
		t.Fatalf("enabled: %v err=%v", enabled, err)
	}
	// setup
	secret, err := repo.SetupEmployee2FA(ctx, empID, f.now)
	if err != nil || len(secret) < 16 {
		t.Fatalf("secret: %q err=%v", secret, err)
	}
	// 错误验证码拒绝
	if err := repo.VerifyEmployee2FA(ctx, empID, "000000", f.now); err == nil {
		t.Fatal("expected error for wrong code")
	}
	// 仍未启用
	enabled, _ = repo.Employee2FAEnabled(ctx, empID)
	if enabled {
		t.Fatal("should not be enabled after wrong code")
	}
	// 用当前时间的真实 TOTP 验证码启用（通过内部算法推导）
	code := currentTOTP(t, secret, f.now)
	if err := repo.VerifyEmployee2FA(ctx, empID, code, f.now); err != nil {
		t.Fatalf("verify: %v", err)
	}
	enabled, _ = repo.Employee2FAEnabled(ctx, empID)
	if !enabled {
		t.Fatal("should be enabled after correct code")
	}
	// 登录时校验
	if err := repo.CheckEmployee2FACode(ctx, empID, code, f.now); err != nil {
		t.Fatalf("check: %v", err)
	}
	if err := repo.CheckEmployee2FACode(ctx, empID, "000000", f.now); err == nil {
		t.Fatal("expected error for wrong login code")
	}
}

// currentTOTP 用测试密钥按 RFC 6238 计算当前验证码（与 store 实现一致）。
func currentTOTP(t *testing.T, secret string, now time.Time) string {
	t.Helper()
	// 直接调用 store 的内部函数不可行，这里用相同的算法重新实现
	key, err := base32Decode(secret)
	if err != nil {
		t.Fatal(err)
	}
	counter := now.Unix() / 30
	return totpForTest(key, counter)
}

func base32Decode(s string) ([]byte, error) {
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(s))
}

func totpForTest(key []byte, counter int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[offset : offset+4]) & 0x7fffffff
	code := bin % 1000000
	str := strings.Repeat("0", 6-len(itoaTest(code))) + itoaTest(code)
	return str
}

func itoaTest(n uint32) string {
	if n == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
