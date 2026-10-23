package httpapi

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClinicalDateRange(t *testing.T) {
	for _, tc := range []struct {
		query         string
		valid, active bool
	}{
		{"", true, false}, {"?dateFrom=2026-10-01&dateTo=2026-10-01", true, true},
		{"?dateFrom=2028-02-29&dateTo=2028-03-01", true, true},
		{"?dateFrom=2026-02-29&dateTo=2026-03-01", false, false},
		{"?dateFrom=2026-10-02&dateTo=2026-10-01", false, false},
		{"?dateFrom=2026-10-01", false, false}, {"?dateFrom=&dateTo=", false, false},
		{"?dateFrom=2026-10-01&dateTo=2026-10-01&dateTo=2026-10-02", false, false},
		{"?dateFrom=0000-01-01&dateTo=0000-01-02", false, false},
	} {
		t.Run(tc.query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("GET", "/"+tc.query, nil)
			if !validQuery(c, "dateFrom", "dateTo") {
				if tc.valid {
					t.Fatal("valid query rejected")
				}
				return
			}
			dates, ok := clinicalDateRange(c)
			if ok != tc.valid || dates.Active() != tc.active {
				t.Fatalf("range=%+v valid=%v", dates, ok)
			}
			if tc.active {
				if dates.From.Hour() != 0 || dates.Until.Hour() != 0 {
					t.Fatal("boundaries are not local midnight")
				}
				_, offset := dates.From.Zone()
				if offset != 8*3600 {
					t.Fatal("incorrect timezone")
				}
				if tc.query == "?dateFrom=2026-10-01&dateTo=2026-10-01" && dates.Until.Sub(dates.From) != 24*time.Hour {
					t.Fatal("end date is not inclusive")
				}
			}
		})
	}
}
