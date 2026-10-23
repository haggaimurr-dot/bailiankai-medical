package wechat

import (
	"context"
	"example.com/bailiankai/api/internal/model"
	"net/url"
	"strconv"
	"time"
)

func (c *Client) SendReminder(ctx context.Context, openID string, t model.MessageTemplate, due time.Time) (string, string) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return "token_unavailable", ""
	}
	title := "复诊提醒"
	if t.Kind == "followup" {
		title = "随访提醒"
	}
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	body := map[string]any{"touser": openID, "template_id": t.TemplateID, "page": t.Page, "miniprogram_state": t.State, "lang": "zh_CN", "data": map[string]any{t.TitleKey: map[string]string{"value": title}, t.TimeKey: map[string]string{"value": due.In(zone).Format("2006-01-02 15:04")}}}
	var result struct {
		ErrCode int `json:"errcode"`
	}
	if err := c.call(ctx, "POST", "/cgi-bin/message/subscribe/send?access_token="+url.QueryEscape(token), body, &result); err != nil {
		return "unknown", ""
	}
	return strconv.Itoa(result.ErrCode), ""
}
