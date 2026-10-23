package store

import (
	"context"
	"fmt"
	"hash/crc64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	cos "github.com/tencentyun/cos-go-sdk-v5"
)

func mockCOS(t *testing.T, handler http.HandlerFunc) *COSMedia {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	return &COSMedia{client: cos.NewClient(&cos.BaseURL{BucketURL: u}, server.Client()), prefix: "private-media/"}
}

func TestCOSPrivateBucketChecks(t *testing.T) {
	for _, test := range []struct {
		name, grants, policy, version string
		wantError                     bool
	}{
		{"private", `<Grant><Grantee type="CanonicalUser"><ID>owner</ID></Grantee><Permission>FULL_CONTROL</Permission></Grant>`, "", "Enabled", false},
		{"public ACL", `<Grant><Grantee type="Group"><URI>http://cam.qcloud.com/groups/global/AllUsers</URI></Grantee><Permission>READ</Permission></Grant>`, "", "Enabled", true},
		{"public policy", "", `{"statement":[{"effect":"allow","principal":{"qcs":["qcs::cam::anyone:anyone"]}}]}`, "Enabled", true},
		{"wildcard policy", "", `{"statement":[{"effect":"allow","principal":{"qcs":["*"]}}]}`, "Enabled", true},
		{"versioning suspended", "", "", "Suspended", true},
		{"private policy", "", `{"statement":[{"effect":"allow","principal":{"qcs":["qcs::cam::uin/123:uin/456"]}}]}`, "Enabled", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			media := mockCOS(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Query().Has("acl"):
					fmt.Fprintf(w, "<AccessControlPolicy><Owner><ID>owner</ID></Owner><AccessControlList>%s</AccessControlList></AccessControlPolicy>", test.grants)
				case r.URL.Query().Has("policy"):
					if test.policy == "" {
						w.WriteHeader(404)
						fmt.Fprint(w, "<Error><Code>NoSuchBucketPolicy</Code></Error>")
					} else {
						fmt.Fprint(w, test.policy)
					}
				case r.URL.Query().Has("versioning"):
					fmt.Fprintf(w, "<VersioningConfiguration><Status>%s</Status></VersioningConfiguration>", test.version)
				default:
					t.Errorf("unexpected request %s", r.URL)
				}
			})
			if err := media.Ready(context.Background()); (err != nil) != test.wantError {
				t.Fatalf("ready=%v wantError=%v", err, test.wantError)
			}
		})
	}
}

func TestCOSUploadHeadersAndPaginatedInventory(t *testing.T) {
	puts, pages := 0, 0
	media := mockCOS(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			puts++
			if r.URL.Path != "/private-media/photo-test.jpg" || r.Header.Get("x-cos-acl") != "private" || r.Header.Get("x-cos-server-side-encryption") != "AES256" || r.Header.Get("Cache-Control") != "no-store" || r.Header.Get("Content-MD5") == "" {
				t.Errorf("unsafe upload %s %v", r.URL.Path, r.Header)
			}
			data, _ := io.ReadAll(r.Body)
			w.Header().Set("x-cos-hash-crc64ecma", fmt.Sprint(crc64.Checksum(data, crc64.MakeTable(crc64.ECMA))))
			if string(data) != "photo bytes" {
				t.Errorf("wrong upload %q", data)
			}
			return
		}
		pages++
		if r.URL.Query().Get("prefix") != "private-media/" {
			t.Error("inventory is not prefix-scoped")
		}
		if pages == 1 {
			fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextMarker>private-media/photo-a.jpg</NextMarker><Contents><Key>private-media/photo-a.jpg</Key><LastModified>2026-10-01T00:00:00.000Z</LastModified></Contents></ListBucketResult>`)
		} else {
			if r.URL.Query().Get("marker") != "private-media/photo-a.jpg" {
				t.Error("pagination lost marker")
			}
			fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>private-media/photo-b.jpg</Key><LastModified>2026-10-01T00:00:00.000Z</LastModified></Contents><Contents><Key>private-media/snapshots/photo-other.jpg</Key><LastModified>2026-10-01T00:00:00.000Z</LastModified></Contents></ListBucketResult>`)
		}
	})
	if err := media.Save(context.Background(), "photo-test", []byte("photo bytes")); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	if err := media.List(context.Background(), func(object MediaObject) error { ids = append(ids, object.ID); return nil }); err != nil {
		t.Fatal(err)
	}
	if puts != 1 || pages != 2 || strings.Join(ids, ",") != "photo-a,photo-b" {
		t.Fatalf("puts=%d pages=%d ids=%v", puts, pages, ids)
	}
}

func TestCOSRejectsUnsafeConfiguration(t *testing.T) {
	base := COSConfig{Bucket: "medical-123456", Region: "ap-shanghai", Prefix: "private-media", SecretID: "test-id", SecretKey: "test-key"}
	if _, err := NewCOSMedia(base); err != nil {
		t.Fatal(err)
	}
	for _, test := range []COSConfig{
		{Bucket: "evil.com/path", Region: base.Region, Prefix: base.Prefix, SecretID: base.SecretID, SecretKey: base.SecretKey},
		{Bucket: base.Bucket, Region: "evil.com", Prefix: base.Prefix, SecretID: base.SecretID, SecretKey: base.SecretKey},
		{Bucket: base.Bucket, Region: base.Region, Prefix: "../public", SecretID: base.SecretID, SecretKey: base.SecretKey},
		{Bucket: base.Bucket, Region: base.Region, Prefix: base.Prefix},
	} {
		if _, err := NewCOSMedia(test); err == nil {
			t.Fatalf("invalid config accepted")
		}
	}
}
