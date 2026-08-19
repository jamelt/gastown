package cmd

import (
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/mail"
)

func TestHasReplyPrefix(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"Re: Status", true},
		{"re: status", true},
		{"RE:status", true},
		{"  Re: leading ws", true},
		{"Reply: not a Re prefix", false},
		{"Status", false},
		{"", false},
		{"Re", false},
		{":empty", false},
	}
	for _, c := range cases {
		if got := hasReplyPrefix(c.in); got != c.want {
			t.Errorf("hasReplyPrefix(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestNormalizeReplySubject(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Re: Hello", "hello"},
		{"re:hello", "hello"},
		{"Re: Re: Re: nested", "nested"},
		{"  Re:  spaced  ", "spaced"},
		{"plain subject", "plain subject"},
		{"", ""},
		{"Re: ", ""},
	}
	for _, c := range cases {
		if got := normalizeReplySubject(c.in); got != c.want {
			t.Errorf("normalizeReplySubject(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeAddress(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"mayor/", "mayor"},
		{"Mayor", "mayor"},
		{"  gastown/Toast/  ", "gastown/toast"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeAddress(c.in); got != c.want {
			t.Errorf("normalizeAddress(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolveSender(t *testing.T) {
	cases := []struct {
		name       string
		mailFrom   string
		detected   string
		want       string
		wantErrHas string
	}{
		{"no override uses detected", "", "gastown/polecats/toast", "gastown/polecats/toast", ""},
		{"redundant self-override is a no-op", "gastown/polecats/toast", "gastown/polecats/toast", "gastown/polecats/toast", ""},
		{"convoy override allowed", "convoy/gt-1234", "gastown/refinery", "convoy/gt-1234", ""},
		{"bare convoy prefix allowed", "convoy/", "gastown/refinery", "convoy/", ""},
		{"prefix-of-detected-not-exact-match rejected", "gastown/polecats/toast-evil", "gastown/polecats/toast", "", "not permitted"},
		{"impersonating overseer rejected", "overseer", "gastown/polecats/toast", "", "not permitted"},
		{"impersonating mayor rejected", "mayor/", "gastown/polecats/toast", "", "not permitted"},
		{"case-sensitive prefix rejected", "Convoy/gt-1234", "gastown/refinery", "", "not permitted"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveSender(c.mailFrom, c.detected)
			if c.wantErrHas != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErrHas) {
					t.Fatalf("resolveSender(%q, %q) error = %v, want error containing %q", c.mailFrom, c.detected, err, c.wantErrHas)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveSender(%q, %q) unexpected error: %v", c.mailFrom, c.detected, err)
			}
			if got != c.want {
				t.Errorf("resolveSender(%q, %q) = %q, want %q", c.mailFrom, c.detected, got, c.want)
			}
		})
	}
}

func TestPickReplyTo(t *testing.T) {
	msg := func(id, from, subj string) *mail.Message {
		return &mail.Message{ID: id, From: from, Subject: subj}
	}

	t.Run("exact match returns id", func(t *testing.T) {
		msgs := []*mail.Message{
			msg("bd-1", "deacon/", "Build broken"),
			msg("bd-2", "witness/", "Build broken"),
		}
		if got := pickReplyTo(msgs, "deacon/", "Re: Build broken"); got != "bd-1" {
			t.Errorf("got %q, want bd-1", got)
		}
	})

	t.Run("address normalization", func(t *testing.T) {
		msgs := []*mail.Message{msg("bd-3", "Mayor", "[HIGH] alert")}
		if got := pickReplyTo(msgs, "mayor/", "Re: [HIGH] alert"); got != "bd-3" {
			t.Errorf("got %q, want bd-3", got)
		}
	})

	t.Run("nested Re prefixes normalize", func(t *testing.T) {
		msgs := []*mail.Message{msg("bd-4", "deacon/", "Re: original")}
		if got := pickReplyTo(msgs, "deacon/", "Re: Re: original"); got != "bd-4" {
			t.Errorf("got %q, want bd-4", got)
		}
	})

	t.Run("ambiguous match returns empty", func(t *testing.T) {
		msgs := []*mail.Message{
			msg("bd-5", "deacon/", "stuck"),
			msg("bd-6", "deacon/", "stuck"),
		}
		if got := pickReplyTo(msgs, "deacon/", "Re: stuck"); got != "" {
			t.Errorf("got %q, want empty (ambiguous)", got)
		}
	})

	t.Run("wrong sender returns empty", func(t *testing.T) {
		msgs := []*mail.Message{msg("bd-7", "witness/", "important")}
		if got := pickReplyTo(msgs, "deacon/", "Re: important"); got != "" {
			t.Errorf("got %q, want empty (wrong sender)", got)
		}
	})

	t.Run("wrong subject returns empty", func(t *testing.T) {
		msgs := []*mail.Message{msg("bd-8", "deacon/", "alpha")}
		if got := pickReplyTo(msgs, "deacon/", "Re: beta"); got != "" {
			t.Errorf("got %q, want empty (wrong subject)", got)
		}
	})

	t.Run("empty subject after strip returns empty", func(t *testing.T) {
		msgs := []*mail.Message{msg("bd-9", "deacon/", "")}
		if got := pickReplyTo(msgs, "deacon/", "Re: "); got != "" {
			t.Errorf("got %q, want empty (degenerate)", got)
		}
	})

	t.Run("empty message list returns empty", func(t *testing.T) {
		if got := pickReplyTo(nil, "deacon/", "Re: anything"); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}
