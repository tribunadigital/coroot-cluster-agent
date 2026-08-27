package mongo

import "testing"

func TestNormalizeAppName(t *testing.T) {
	cases := map[string]string{
		"QAN-mongodb-profiler-5b67479c-c030-4c77-bbbc-b5f1f92025be": "QAN-mongodb-profiler",
		"QAN-mongodb-profiler-ace7bc2c-5973-4708-a55b-33452e6":      "QAN-mongodb-profiler",
		"mongodb_exporter":                     "mongodb_exporter",
		"mongosh 2.9.2":                        "mongosh 2.9.2",
		"review-service":                       "review-service",
		"review-service-6b8f9d4c7d-xq2mn":      "review-service",
		"api-gateway-7d9f8c6b5-tkn4z":          "api-gateway",
		"mongodb-rs0-2":                        "mongodb-rs0-2",
		"my-application-server":                "my-application-server",
		"app-deadbeef-cafe":                    "app-deadbeef-cafe",
		"worker-507f1f77bcf86cd799439011":      "worker",
		"5b67479c-c030-4c77-bbbc-b5f1f92025be": "unknown",
		"":                                     "unknown",
	}
	for in, want := range cases {
		if got := normalizeAppName(in); got != want {
			t.Errorf("normalizeAppName(%q) = %q, want %q", in, got, want)
		}
	}
}
