package store

import "testing"

func TestScheduleSettingValidation(t *testing.T) {
	good := ScheduleSetting{Templates: []ScheduleTemplate{{ID: "t", Name: "上午班", DoctorID: "doctor", Weekdays: []int{1, 2}, Start: "09:00", End: "12:00", Duration: 30, Capacity: 3, Open: true}}, Holidays: []HolidayRule{{Date: "2099-01-01", Name: "休诊", Kind: "rest"}}}
	if !validScheduleSetting(good) {
		t.Fatal("valid setting rejected")
	}
	for _, change := range []func(*ScheduleSetting){
		func(s *ScheduleSetting) { s.Templates[0].Duration = 0 },
		func(s *ScheduleSetting) { s.Templates[0].Duration = 31 },
		func(s *ScheduleSetting) { s.Templates[0].Start = "25:00" },
		func(s *ScheduleSetting) { s.Templates[0].Weekdays = []int{1, 1} },
		func(s *ScheduleSetting) { s.Templates[0].Capacity = 101 },
		func(s *ScheduleSetting) { s.Holidays[0].Date = "2099-02-30" },
		func(s *ScheduleSetting) { s.Holidays = append(s.Holidays, s.Holidays[0]) },
		func(s *ScheduleSetting) { s.Holidays[0].Kind = "unknown" },
	} {
		in := good
		in.Templates = append([]ScheduleTemplate(nil), good.Templates...)
		in.Holidays = append([]HolidayRule(nil), good.Holidays...)
		change(&in)
		if validScheduleSetting(in) {
			t.Fatalf("invalid setting accepted: %+v", in)
		}
	}
}
