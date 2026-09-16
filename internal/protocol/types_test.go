package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTargetListValidate(t *testing.T) {
	targets := TargetList{
		Version:  Version,
		ConfigID: "23ea604f-6e47-5710-bc10-ab9b6a1302a3",
		Config: MeasurementConfig{
			IntervalMS: 10_000,
			ICMP:       ICMPConfig{Count: 5, IntervalMS: 200, TimeoutMS: 1_000},
			HTTP:       HTTPConfig{Method: "GET", FollowRedirects: true, VerifyTLS: true, TimeoutMS: 5_000},
		},
		Targets: []Target{
			{TargetID: "aliyun_dns", Address: "223.5.5.5", ProbeTypes: []string{"icmp"}},
			{TargetID: "cqu_mirror", Address: "https://mirrors.cqu.edu.cn/", ProbeTypes: []string{"http"}},
		},
	}
	if err := targets.Validate(); err != nil {
		t.Fatalf("valid target list rejected: %v", err)
	}

	targets.Targets[1].TargetID = "aliyun_dns"
	if err := targets.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate target error, got %v", err)
	}
}

func TestICMPFailureMarshalsNullDurations(t *testing.T) {
	data, err := json.Marshal(ICMPResult{Success: false, Sent: 5, Received: 0, LossRatio: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"success":false,"sent":5,"received":0,"loss_ratio":1,"min_rtt_ms":null,"avg_rtt_ms":null,"max_rtt_ms":null,"jitter_ms":null}`
	if string(data) != want {
		t.Fatalf("unexpected JSON\nwant: %s\n got: %s", want, data)
	}
}

func TestICMPRoundMustFitInterval(t *testing.T) {
	targets := TargetList{
		Version:  Version,
		ConfigID: "23ea604f-6e47-5710-bc10-ab9b6a1302a3",
		Config: MeasurementConfig{
			IntervalMS: 1_000,
			ICMP:       ICMPConfig{Count: 2, IntervalMS: 500, TimeoutMS: 500},
			HTTP:       HTTPConfig{Method: "GET", TimeoutMS: 500},
		},
	}
	if err := targets.Validate(); err == nil {
		t.Fatal("expected invalid ICMP schedule")
	}
}

func TestTargetListRequiresUUIDV5ConfigID(t *testing.T) {
	targets := TargetList{
		Version: Version,
		Config: MeasurementConfig{
			IntervalMS: 10_000,
			ICMP:       ICMPConfig{Count: 1, IntervalMS: 1, TimeoutMS: 1_000},
			HTTP:       HTTPConfig{Method: "GET", TimeoutMS: 5_000},
		},
	}
	for _, id := range []string{"", "23ea604f-6e47-4710-bc10-ab9b6a1302a3", "23EA604F-6E47-5710-BC10-AB9B6A1302A3"} {
		targets.ConfigID = id
		if err := targets.Validate(); err == nil {
			t.Fatalf("accepted invalid config_id %q", id)
		}
	}
	targets.ConfigID = "23ea604f-6e47-5710-bc10-ab9b6a1302a3"
	if err := targets.Validate(); err != nil {
		t.Fatalf("rejected valid config_id: %v", err)
	}
}
