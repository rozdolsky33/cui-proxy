package recovery

import "testing"

func TestVaultFailureRequiresStorageRepair(t *testing.T) {
	g := Lookup("vault", "", "")
	if g.Code != "vault" || g.Category != "dependency" || g.Retry != "after_repair" {
		t.Fatal("vault failure not classified as repairable storage dependency")
	}
}
