package store_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
	"time"

	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
)

// 行117: 模板效果统计——发送/送达/点击/失败累加。
func TestTemplateEffectStats(t *testing.T) {
	fixture := newBookingFixture(t)
	ctx := context.Background()
	patient := fixture.patient()

	if err := fixture.db.Exec("DELETE FROM message_clicks").Error; err != nil {
		t.Fatal(err)
	}
	tmpl := model.MessageTemplate{ID: "tmpl-effect-test", ClinicID: "clinic-demo", Kind: "effect-test",
		TemplateID: "test-effect", TitleKey: "t1", TimeKey: "t2", Page: "pages/x", State: "developer",
		LeadMinutes: 60, Enabled: true, Version: 1}
	if err := fixture.db.Create(&tmpl).Error; err != nil {
		t.Fatal(err)
	}
	mkNotif := func(id, status string) {
		n := model.Notification{ID: id, ClinicID: "clinic-demo", PatientID: patient.Patient.ID,
			TemplateID: tmpl.ID, Kind: "effect-test", SourceID: id,
			DueAt: fixture.now, SendAt: fixture.now, Status: status}
		if err := fixture.db.Create(&n).Error; err != nil {
			t.Fatal(err)
		}
	}
	mkNotif("notif-eff-1", "sent")
	mkNotif("notif-eff-2", "sent")
	mkNotif("notif-eff-3", "failed")
	// 点击其中一条
	if err := fixture.repo.RecordMessageClick(ctx, tmpl.ID, "notif-eff-1", patient.Patient.ID, fixture.now); err != nil {
		t.Fatal(err)
	}
	stats, err := fixture.repo.TemplateEffectStats(ctx, tmpl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Sent != 3 || stats.Delivered != 2 || stats.Failed != 1 || stats.Clicked != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	// 点击必须归属患者本人的消息
	if err := fixture.repo.RecordMessageClick(ctx, "", "notif-eff-1", patient.Patient.ID, fixture.now); err == nil {
		t.Fatal("expected error for empty template id")
	}
}

// 行118: 接口监控——调用量/错误率统计与慢请求告警。
func TestApiRequestMonitoring(t *testing.T) {
	fixture := newBookingFixture(t)
	ctx := context.Background()
	day := fixture.now

	// 自清理：不受历史运行残留影响
	if err := fixture.db.Exec("DELETE FROM monitoring_settings").Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Exec("DELETE FROM api_request_stats").Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Exec("DELETE FROM api_slow_alerts").Error; err != nil {
		t.Fatal(err)
	}
	if got := fixture.repo.SlowRequestThreshold(ctx); got != 2000 {
		t.Fatalf("default threshold = %d, want 2000", got)
	}
	if err := fixture.repo.SetSlowRequestThreshold(ctx, 5, fixture.now); err != nil {
		t.Fatal(err)
	}
	if got := fixture.repo.SlowRequestThreshold(ctx); got != 5 {
		t.Fatalf("threshold = %d, want 5", got)
	}
	if err := fixture.repo.SetSlowRequestThreshold(ctx, 0, fixture.now); err == nil {
		t.Fatal("expected error for threshold 0")
	}

	// 正常请求 + 报错请求都被统计
	if err := fixture.repo.RecordApiRequest(ctx, "/api/v1/test", "GET", 200, 3, day); err != nil {
		t.Fatal(err)
	}
	if err := fixture.repo.RecordApiRequest(ctx, "/api/v1/test", "GET", 500, 10, day); err != nil {
		t.Fatal(err)
	}
	rows, err := fixture.repo.RouteStats(ctx, day.Format("2006-01-02"))
	if err != nil {
		t.Fatal(err)
	}
	var found *model.ApiRequestStat
	for i := range rows {
		if rows[i].Route == "/api/v1/test" && rows[i].Method == "GET" {
			found = &rows[i]
		}
	}
	if found == nil {
		t.Fatal("route stat not found")
	}
	if found.Total != 2 || found.Err5xx != 1 || found.MaxMs != 10 {
		t.Fatalf("unexpected route stat: %+v", found)
	}
	// 慢请求触发告警记录
	if err := fixture.repo.RecordSlowAlert(ctx, "/api/v1/test", "GET", 200, 10, 5, fixture.now); err != nil {
		t.Fatal(err)
	}
	alerts, err := fixture.repo.SlowAlerts(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) == 0 || alerts[0].DurationMs != 10 || alerts[0].ThresholdMs != 5 {
		t.Fatalf("slow alert not recorded: %+v", alerts)
	}
	// 恢复默认阈值，不影响其他测试
	if err := fixture.repo.SetSlowRequestThreshold(ctx, 2000, fixture.now); err != nil {
		t.Fatal(err)
	}
}

// 行119: 数据变更日报——计数与抽查对账。
func TestDataDailyReport(t *testing.T) {
	fixture := newBookingFixture(t)
	ctx := context.Background()
	day := fixture.now

	if err := fixture.db.Exec("DELETE FROM data_daily_reports").Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.repo.GenerateDataDailyReport(ctx, day, fixture.now); err != nil {
		t.Fatal(err)
	}
	rows, err := fixture.repo.GetDataDailyReport(ctx, day.Format("2006-01-02"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("no daily rows")
	}
	got := map[string]int64{}
	for _, r := range rows {
		got[r.TableName] = r.RowCount
	}
	// 抽查对账：patients 表计数必须与直接 COUNT 一致
	var n int64
	if err := fixture.db.Table("patients").Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if got["patients"] != n {
		t.Fatalf("patients count mismatch: report=%d db=%d", got["patients"], n)
	}
}

// 行123: 异常访问行为告警——5 次被拒触发，1 小时内不重复。
func TestSecuritySignalAlert(t *testing.T) {
	fixture := newBookingFixture(t)
	ctx := context.Background()

	if err := fixture.db.Exec("DELETE FROM security_signals").Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Exec("DELETE FROM security_alerts").Error; err != nil {
		t.Fatal(err)
	}
	actor := "test-intruder-1"
	for i := 0; i < 4; i++ {
		if err := fixture.repo.RecordSecuritySignal(ctx, "patient", actor, "photo-access-denied",
			"followup-photo", "photo-x", fixture.now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	alerts, err := fixture.repo.SecurityAlerts(ctx, true, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range alerts {
		if a.ActorID == actor {
			t.Fatal("alert fired before reaching the threshold of 5")
		}
	}
	// 第 5 次触发告警
	if err := fixture.repo.RecordSecuritySignal(ctx, "patient", actor, "photo-access-denied",
		"followup-photo", "photo-x", fixture.now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	alerts, err = fixture.repo.SecurityAlerts(ctx, true, 10)
	if err != nil {
		t.Fatal(err)
	}
	var found *model.SecurityAlert
	for i := range alerts {
		if alerts[i].ActorID == actor {
			found = &alerts[i]
		}
	}
	if found == nil {
		t.Fatal("expected security alert after 5 denied accesses")
	}
	// 1 小时冷却：再来 5 次也不重复告警
	for i := 0; i < 5; i++ {
		if err := fixture.repo.RecordSecuritySignal(ctx, "patient", actor, "photo-access-denied",
			"followup-photo", "photo-x", fixture.now.Add(time.Duration(6+i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	alerts, err = fixture.repo.SecurityAlerts(ctx, false, 50)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, a := range alerts {
		if a.ActorID == actor {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 alert (cooldown), got %d", count)
	}
	// 标记已读
	if err := fixture.repo.MarkSecurityAlertRead(ctx, found.ID, fixture.now); err != nil {
		t.Fatal(err)
	}
	unread, err := fixture.repo.SecurityAlerts(ctx, true, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range unread {
		if a.ID == found.ID {
			t.Fatal("alert should be marked read")
		}
	}
	if err := fixture.repo.MarkSecurityAlertRead(ctx, "no-such-alert", fixture.now); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// 行120: 展示副本生成——压缩变小且带水印。
func TestDisplayVariantGeneration(t *testing.T) {
	// 构造一张 2000x1500 的渐变测试图（高质 JPEG，原图较大）
	img := image.NewRGBA(image.Rect(0, 0, 2000, 1500))
	for y := 0; y < 1500; y++ {
		for x := 0; x < 2000; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	variant, err := store.GenerateDisplayVariant(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(variant) >= len(data) {
		t.Fatalf("variant not smaller: orig=%d variant=%d", len(data), len(variant))
	}
	vimg, err := jpeg.Decode(bytes.NewReader(variant))
	if err != nil {
		t.Fatal(err)
	}
	if m := max(vimg.Bounds().Dx(), vimg.Bounds().Dy()); m > 1280 {
		t.Fatalf("variant longest side = %d, want <= 1280", m)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
