package config

import (
	"fmt"
	"testing"
)

func TestLoadRejectsTrailingYAMLDocuments(t *testing.T) {
	for _, tail := range []string{"---\npolicy: {profile: strict}\n", "---\n", "---\ninvalid: ["} {
		if c, err := Load(writeTemp(t, "policy: {profile: sanitize}\n"+tail)); err == nil {
			t.Errorf("accepted extra document with effective profile %s", c.Policy.Profile)
		}
	}
}

func TestLoadAuditRetentionBounds(t *testing.T) {
	for _, tc := range []struct {
		days int
		ok   bool
	}{{0, true}, {90, true}, {106751, true}, {106752, false}, {int(^uint(0) >> 1), false}} {
		t.Run(fmt.Sprint(tc.days), func(t *testing.T) {
			_, err := Load(writeTemp(t, fmt.Sprintf("audit: {retention_days: %d}\n", tc.days)))
			if (err == nil) != tc.ok {
				t.Errorf("retention_days=%d: err=%v, want valid=%v", tc.days, err, tc.ok)
			}
		})
	}
}

func TestValidateListenPorts(t *testing.T) {
	for _, field := range []string{"listen", "ui.listen"} {
		for _, port := range []string{"-1", "65536", "99999999999999999999", "no-such-cui-service"} {
			t.Run(field+"/"+port, func(t *testing.T) {
				c := base()
				if field == "listen" {
					c.Listen = "127.0.0.1:" + port
				} else {
					c.UI.Listen = "127.0.0.1:" + port
				}
				if err := c.Validate(); err == nil {
					t.Errorf("accepted invalid %s port %q", field, port)
				}
			})
		}
	}
	for _, port := range []string{"0", "65535", "http"} {
		c := base()
		c.Listen = "127.0.0.1:" + port
		if err := c.Validate(); err != nil {
			t.Errorf("valid port %q: %v", port, err)
		}
	}
}
