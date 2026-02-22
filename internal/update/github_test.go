package update

import "testing"

func TestSelectBinaryAssetByArch(t *testing.T) {
	release := ReleaseMetadata{
		Tag: "v1.2.3",
		Assets: []ReleaseAsset{
			{Name: "SHA256SUMS"},
			{Name: "split-vpn-webui-linux-arm64"},
			{Name: "split-vpn-webui-linux-amd64"},
		},
	}
	asset, err := selectBinaryAsset(release, "arm64")
	if err != nil {
		t.Fatalf("selectBinaryAsset failed: %v", err)
	}
	if asset.Name != "split-vpn-webui-linux-arm64" {
		t.Fatalf("unexpected asset selected: %q", asset.Name)
	}
}
