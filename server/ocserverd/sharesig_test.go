package main

import (
	"encoding/hex"
	"testing"
)

func TestDeriveShareKey(t *testing.T) {
	t.Run("a signing secret produces the fixed share-domain key", func(t *testing.T) {
		got := deriveShareKey([]byte("server signing secret"))
		if gotHex := hex.EncodeToString(got); gotHex != "78180fde953b263d1deb354bbd6277713925f0bc80b2ebdbc14615aafe7b92d5" {
			t.Fatalf("derived share key = %s, want %s", gotHex, "78180fde953b263d1deb354bbd6277713925f0bc80b2ebdbc14615aafe7b92d5")
		}
	})

	t.Run("changing the signing secret changes the share-domain key", func(t *testing.T) {
		want := "78180fde953b263d1deb354bbd6277713925f0bc80b2ebdbc14615aafe7b92d5"
		if got := hex.EncodeToString(deriveShareKey([]byte("another signing secret"))); got == want {
			t.Fatalf("different signing secret produced the original share key %s", got)
		}
	})
}

func TestShareSigFor(t *testing.T) {
	t.Run("an attachment id produces its fixed truncated base64url signature", func(t *testing.T) {
		got := shareSigFor([]byte("server signing secret"), "att-0123456789ab")
		if got != "OdEIRovZZwqhTD2ISM7DYczpCzQc-vSQ" {
			t.Fatalf("share signature = %q, want %q", got, "OdEIRovZZwqhTD2ISM7DYczpCzQc-vSQ")
		}
		if len(got) != 32 {
			t.Fatalf("share signature length = %d, want 32", len(got))
		}
	})

	t.Run("changing the attachment id changes the signature", func(t *testing.T) {
		want := "OdEIRovZZwqhTD2ISM7DYczpCzQc-vSQ"
		if got := shareSigFor([]byte("server signing secret"), "att-0123456789ac"); got == want {
			t.Fatalf("different attachment id produced the original signature %q", got)
		}
	})

	t.Run("changing the signing secret changes the signature", func(t *testing.T) {
		want := "OdEIRovZZwqhTD2ISM7DYczpCzQc-vSQ"
		if got := shareSigFor([]byte("another signing secret"), "att-0123456789ab"); got == want {
			t.Fatalf("different signing secret produced the original signature %q", got)
		}
	})
}

func TestDeriveDiffKey(t *testing.T) {
	t.Run("a signing secret produces the fixed comparison-domain key", func(t *testing.T) {
		got := deriveDiffKey([]byte("server signing secret"))
		if gotHex := hex.EncodeToString(got); gotHex != "2b4932558851c405cfa8557d925d5cef6ea573bd0ef386bc90e9bd69ea85f77a" {
			t.Fatalf("derived diff key = %s, want %s", gotHex, "2b4932558851c405cfa8557d925d5cef6ea573bd0ef386bc90e9bd69ea85f77a")
		}
		if gotHex := hex.EncodeToString(got); gotHex == "78180fde953b263d1deb354bbd6277713925f0bc80b2ebdbc14615aafe7b92d5" {
			t.Fatalf("comparison-domain key reused the share-domain key: %s", gotHex)
		}
	})

	t.Run("changing the signing secret changes the comparison-domain key", func(t *testing.T) {
		want := "2b4932558851c405cfa8557d925d5cef6ea573bd0ef386bc90e9bd69ea85f77a"
		if got := hex.EncodeToString(deriveDiffKey([]byte("another signing secret"))); got == want {
			t.Fatalf("different signing secret produced the original diff key %s", got)
		}
	})
}

func TestDiffSigPayload(t *testing.T) {
	t.Run("all comparison fields are sorted and percent encoded", func(t *testing.T) {
		got := diffSigPayload("att-0123456789ab", "doc:global_context/global/current/text", "初始 & before", "after=now")
		want := "after=doc%3Aglobal_context%2Fglobal%2Fcurrent%2Ftext&before=att-0123456789ab&label_after=after%3Dnow&label_before=%E5%88%9D%E5%A7%8B+%26+before"
		if got != want {
			t.Fatalf("diff signature payload = %q, want %q", got, want)
		}
	})

	t.Run("empty labels remain explicit signed fields", func(t *testing.T) {
		got := diffSigPayload("before value", "after/value", "", "")
		want := "after=after%2Fvalue&before=before+value&label_after=&label_before="
		if got != want {
			t.Fatalf("empty-label payload = %q, want %q", got, want)
		}
	})

	t.Run("changing a comparison field changes the canonical payload", func(t *testing.T) {
		want := "after=doc%3Aglobal_context%2Fglobal%2Fcurrent%2Ftext&before=att-0123456789ab&label_after=after%3Dnow&label_before=%E5%88%9D%E5%A7%8B+%26+before"
		if got := diffSigPayload("att-0123456789ab", "doc:global_context/global/current/text", "初始 & before", "after=next"); got == want {
			t.Fatalf("changed comparison field preserved the original payload %q", got)
		}
	})
}

func TestDiffSigFor(t *testing.T) {
	t.Run("the canonical comparison payload produces its fixed truncated signature", func(t *testing.T) {
		got := diffSigFor([]byte("server signing secret"), "att-0123456789ab", "doc:global_context/global/current/text", "初始 & before", "after=now")
		if got != "QT3hJfJhA9KQYsdk5Vx-Owo15k52uzQz" {
			t.Fatalf("diff signature = %q, want %q", got, "QT3hJfJhA9KQYsdk5Vx-Owo15k52uzQz")
		}
		if len(got) != 32 {
			t.Fatalf("diff signature length = %d, want 32", len(got))
		}
	})

	t.Run("changing a column label changes the comparison signature", func(t *testing.T) {
		want := "QT3hJfJhA9KQYsdk5Vx-Owo15k52uzQz"
		if got := diffSigFor([]byte("server signing secret"), "att-0123456789ab", "doc:global_context/global/current/text", "初始 & before", "after=next"); got == want {
			t.Fatalf("changed column label produced the original signature %q", got)
		}
	})

	t.Run("changing the signing secret changes the comparison signature", func(t *testing.T) {
		want := "QT3hJfJhA9KQYsdk5Vx-Owo15k52uzQz"
		if got := diffSigFor([]byte("another signing secret"), "att-0123456789ab", "doc:global_context/global/current/text", "初始 & before", "after=now"); got == want {
			t.Fatalf("different signing secret produced the original signature %q", got)
		}
	})
}

func TestShareSigForRing(t *testing.T) {
	t.Run("a new attachment link uses the currently signing key", func(t *testing.T) {
		kr := newKeyring([]signingKey{{ID: "k-old", Key: []byte("old")}, {ID: "k-active", Key: []byte("active")}}, "k-active")
		got := shareSigForRing(kr, "att-0123456789ab")
		if got != "4FbeWRczQGIj9HZGSsM3I3CE7G7Ceoxn" {
			t.Fatalf("ring share signature = %q, want %q", got, "4FbeWRczQGIj9HZGSsM3I3CE7G7Ceoxn")
		}
		if got := kr.activeKeyID(); got != "k-active" {
			t.Fatalf("active key id after signing = %q, want %q", got, "k-active")
		}
	})

	t.Run("a ring without a signing key returns an empty signature", func(t *testing.T) {
		kr := newKeyring([]signingKey{{ID: "k-old", Key: []byte("old")}}, "k-missing")
		if got := shareSigForRing(kr, "att-0123456789ab"); got != "" {
			t.Fatalf("ring without signing key returned %q, want empty", got)
		}
	})
}

func TestVerifyShareSigAnyKey(t *testing.T) {
	for _, tc := range []struct {
		name       string
		kr         *keyring
		attachment string
		sig        string
		want       bool
		activeID   string
	}{
		{
			name:       "a signature from a retained old key is accepted",
			kr:         newKeyring([]signingKey{{ID: "k-old", Key: []byte("old")}, {ID: "k-active", Key: []byte("active")}}, "k-active"),
			attachment: "att-0123456789ab",
			sig:        "i6MM8by4NN1Mm-7RLdpWd46WGwJTztEE",
			want:       true,
			activeID:   "k-active",
		},
		{
			name:       "a signature from the current key is accepted",
			kr:         newKeyring([]signingKey{{ID: "k-old", Key: []byte("old")}, {ID: "k-active", Key: []byte("active")}}, "k-active"),
			attachment: "att-0123456789ab",
			sig:        "4FbeWRczQGIj9HZGSsM3I3CE7G7Ceoxn",
			want:       true,
			activeID:   "k-active",
		},
		{
			name:       "a signature from a removed key is refused",
			kr:         newKeyring([]signingKey{{ID: "k-active", Key: []byte("active")}}, "k-active"),
			attachment: "att-0123456789ab",
			sig:        "i6MM8by4NN1Mm-7RLdpWd46WGwJTztEE",
			want:       false,
			activeID:   "k-active",
		},
		{
			name:       "a signature for another attachment is refused",
			kr:         newKeyring([]signingKey{{ID: "k-old", Key: []byte("old")}, {ID: "k-active", Key: []byte("active")}}, "k-active"),
			attachment: "att-0123456789ac",
			sig:        "4FbeWRczQGIj9HZGSsM3I3CE7G7Ceoxn",
			want:       false,
			activeID:   "k-active",
		},
		{
			name:       "an empty key ring refuses every signature",
			kr:         newKeyring(nil, ""),
			attachment: "att-0123456789ab",
			sig:        "4FbeWRczQGIj9HZGSsM3I3CE7G7Ceoxn",
			want:       false,
			activeID:   "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := verifyShareSigAnyKey(tc.kr, tc.attachment, tc.sig); got != tc.want {
				t.Fatalf("signature accepted = %v, want %v", got, tc.want)
			}
			if got := tc.kr.activeKeyID(); got != tc.activeID {
				t.Fatalf("active key id after verification = %q, want %q", got, tc.activeID)
			}
		})
	}
}

func TestDiffSigForRing(t *testing.T) {
	t.Run("a new comparison link uses the currently signing key", func(t *testing.T) {
		kr := newKeyring([]signingKey{{ID: "k-old", Key: []byte("old")}, {ID: "k-active", Key: []byte("active")}}, "k-active")
		got := diffSigForRing(kr, "att-0123456789ab", "doc:global_context/global/current/text", "初始 & before", "after=now")
		if got != "JeehcqMuIaaEMvNu_8um3Dw2i83Tfr2_" {
			t.Fatalf("ring diff signature = %q, want %q", got, "JeehcqMuIaaEMvNu_8um3Dw2i83Tfr2_")
		}
		if got := kr.activeKeyID(); got != "k-active" {
			t.Fatalf("active key id after signing = %q, want %q", got, "k-active")
		}
	})

	t.Run("a ring without a signing key returns an empty comparison signature", func(t *testing.T) {
		kr := newKeyring([]signingKey{{ID: "k-old", Key: []byte("old")}}, "k-missing")
		if got := diffSigForRing(kr, "att-0123456789ab", "doc:global_context/global/current/text", "初始 & before", "after=now"); got != "" {
			t.Fatalf("ring without signing key returned %q, want empty", got)
		}
	})
}

func TestVerifyDiffSigAnyKey(t *testing.T) {
	for _, tc := range []struct {
		name        string
		kr          *keyring
		before      string
		after       string
		labelBefore string
		labelAfter  string
		sig         string
		want        bool
		activeID    string
	}{
		{
			name:        "a signature from a retained old key is accepted",
			kr:          newKeyring([]signingKey{{ID: "k-old", Key: []byte("old")}, {ID: "k-active", Key: []byte("active")}}, "k-active"),
			before:      "att-0123456789ab",
			after:       "doc:global_context/global/current/text",
			labelBefore: "初始 & before",
			labelAfter:  "after=now",
			sig:         "PcIWJyXjVm2FcTRoyVtmNxo0Jz3rZQQI",
			want:        true,
			activeID:    "k-active",
		},
		{
			name:        "a signature from the current key is accepted",
			kr:          newKeyring([]signingKey{{ID: "k-old", Key: []byte("old")}, {ID: "k-active", Key: []byte("active")}}, "k-active"),
			before:      "att-0123456789ab",
			after:       "doc:global_context/global/current/text",
			labelBefore: "初始 & before",
			labelAfter:  "after=now",
			sig:         "JeehcqMuIaaEMvNu_8um3Dw2i83Tfr2_",
			want:        true,
			activeID:    "k-active",
		},
		{
			name:        "a signature from a removed key is refused",
			kr:          newKeyring([]signingKey{{ID: "k-active", Key: []byte("active")}}, "k-active"),
			before:      "att-0123456789ab",
			after:       "doc:global_context/global/current/text",
			labelBefore: "初始 & before",
			labelAfter:  "after=now",
			sig:         "PcIWJyXjVm2FcTRoyVtmNxo0Jz3rZQQI",
			want:        false,
			activeID:    "k-active",
		},
		{
			name:        "a signature with a changed column label is refused",
			kr:          newKeyring([]signingKey{{ID: "k-old", Key: []byte("old")}, {ID: "k-active", Key: []byte("active")}}, "k-active"),
			before:      "att-0123456789ab",
			after:       "doc:global_context/global/current/text",
			labelBefore: "初始 & before",
			labelAfter:  "after=next",
			sig:         "JeehcqMuIaaEMvNu_8um3Dw2i83Tfr2_",
			want:        false,
			activeID:    "k-active",
		},
		{
			name:        "an empty key ring refuses every comparison signature",
			kr:          newKeyring(nil, ""),
			before:      "att-0123456789ab",
			after:       "doc:global_context/global/current/text",
			labelBefore: "初始 & before",
			labelAfter:  "after=now",
			sig:         "JeehcqMuIaaEMvNu_8um3Dw2i83Tfr2_",
			want:        false,
			activeID:    "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := verifyDiffSigAnyKey(tc.kr, tc.before, tc.after, tc.labelBefore, tc.labelAfter, tc.sig); got != tc.want {
				t.Fatalf("signature accepted = %v, want %v", got, tc.want)
			}
			if got := tc.kr.activeKeyID(); got != tc.activeID {
				t.Fatalf("active key id after verification = %q, want %q", got, tc.activeID)
			}
		})
	}
}
