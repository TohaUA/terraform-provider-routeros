package routeros

import (
	neturl "net/url"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/go-routeros/routeros/v3"
	"github.com/go-routeros/routeros/v3/proto"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// Debug logs carry request and response bodies, and those carry secrets: a
// password written to a PPP secret, a WireGuard private key the router reads
// back. Sensitive only keeps a value out of plan and apply output. With
// TF_LOG=DEBUG both transports used to write these bodies to the log verbatim,
// which puts credentials into local logs and CI output.
//
// The helpers here replace the value of every field that the provider, a
// resource or a data source marks Sensitive, matched by its RouterOS name,
// before a body is logged. Only the log is redacted: what is sent to the router
// and what is decoded from its reply are left exactly as they were.
//
// Matching is by name across the whole provider, so a field that is sensitive in
// one resource is redacted wherever that name appears. That errs towards hiding
// too much in a debug log, which is the side to err on.

const redactedValue = "***"

var (
	sensitiveNamesOnce sync.Once
	sensitiveNames     map[string]struct{}
	sensitiveJSONField *regexp.Regexp
)

func loadSensitiveNames() {
	sensitiveNamesOnce.Do(func() {
		sensitiveNames = map[string]struct{}{}

		p := Provider()
		collectSensitiveNames(p.Schema)
		for _, r := range p.ResourcesMap {
			collectSensitiveNames(r.Schema)
		}
		for _, r := range p.DataSourcesMap {
			collectSensitiveNames(r.Schema)
		}

		// A sensitive attribute renamed for some RouterOS version is sent under
		// its router name.
		for _, obj := range driftAttributeSlice {
			for _, attrs := range obj.Resources {
				for _, attr := range attrs {
					if _, ok := sensitiveNames[SnakeToKebab(attr.TF)]; ok {
						sensitiveNames[SnakeToKebab(attr.MT)] = struct{}{}
					}
				}
			}
		}

		names := make([]string, 0, len(sensitiveNames))
		for name := range sensitiveNames {
			names = append(names, regexp.QuoteMeta(name))
		}
		sort.Strings(names)

		// A JSON member whose key is a sensitive name -- possibly inside a nested
		// block ("input.auth-key") or marked for unset ("!password") -- and whose
		// value is a non-empty string. An empty value is an unset and has nothing
		// to hide, so it is left as it is rather than logged as if it held a secret.
		sensitiveJSONField = regexp.MustCompile(
			`("!?(?:[a-z0-9/-]+\.)*(?:` + strings.Join(names, "|") + `)"\s*:\s*)"(?:[^"\\]|\\.)+"`)
	})
}

func collectSensitiveNames(s map[string]*schema.Schema) {
	var transform map[string]string
	if ts, ok := s[MetaTransformSet]; ok {
		if def, ok := ts.Default.(string); ok {
			transform = loadTransformSet(def, false)
		}
	}

	for name, field := range s {
		if field.Sensitive {
			sensitiveNames[SnakeToKebab(name)] = struct{}{}

			// The transform set pairs a schema name with a router name; a sensitive
			// field is sent under the router name.
			for a, b := range transform {
				switch name {
				case a:
					sensitiveNames[SnakeToKebab(b)] = struct{}{}
				case b:
					sensitiveNames[SnakeToKebab(a)] = struct{}{}
				}
			}
		}

		if elem, ok := field.Elem.(*schema.Resource); ok {
			collectSensitiveNames(elem.Schema)
		}
	}
}

// isSensitiveName reports whether a RouterOS field name, as it appears on the
// wire, names a sensitive field: "password", "!password" for an unset, or
// "input.auth-key" inside a nested block.
func isSensitiveName(name string) bool {
	loadSensitiveNames()

	name = strings.TrimPrefix(name, "!")
	if _, ok := sensitiveNames[name]; ok {
		return true
	}
	if i := strings.LastIndex(name, "."); i >= 0 {
		_, ok := sensitiveNames[name[i+1:]]
		return ok
	}
	return false
}

// redactAPIWords returns a copy of an API command's words for logging, with the
// value of every sensitive field replaced. That covers attribute words
// ("=name=value") and query words ("?name=value", and "?=name=value",
// "?>name=value", "?<name=value"), which a filtered read or an import builds
// from its filter. The words themselves are what is sent, so they are never
// modified.
func redactAPIWords(words []string) []string {
	out := make([]string, len(words))
	for i, word := range words {
		out[i] = word

		var prefix, rest string
		switch {
		case strings.HasPrefix(word, "="):
			prefix, rest = "=", word[1:]
		case strings.HasPrefix(word, "?"):
			prefix, rest = "?", word[1:]
			if rest != "" && strings.ContainsRune("=<>", rune(rest[0])) {
				prefix, rest = word[:2], rest[1:]
			}
		default:
			continue
		}
		name, value, ok := strings.Cut(rest, "=")
		if ok && value != "" && isSensitiveName(name) {
			out[i] = prefix + name + "=" + redactedValue
		}
	}
	return out
}

// redactURL returns a REST request URL for logs and error messages, with the
// value of every sensitive query parameter replaced. A filtered read or an import
// puts its filter in the query string ("?name=alice&password=..."). That query is
// not escaped, so a segment without '=' that follows a sensitive parameter is
// taken as the rest of its value rather than a parameter of its own. The URL that
// is requested is left as it is.
func redactURL(raw string) string {
	base, query, ok := strings.Cut(raw, "?")
	if !ok {
		return raw
	}

	segments := strings.Split(query, "&")
	out := make([]string, 0, len(segments))
	inSecret := false
	for _, segment := range segments {
		key, value, hasValue := strings.Cut(segment, "=")
		if !hasValue {
			if !inSecret {
				out = append(out, segment)
			}
			continue
		}

		name := key
		if unescaped, err := neturl.QueryUnescape(key); err == nil {
			name = unescaped
		}
		inSecret = value != "" && isSensitiveName(name)
		if inSecret {
			segment = key + "=" + redactedValue
		}
		out = append(out, segment)
	}

	return base + "?" + strings.Join(out, "&")
}

// redactReply renders an API reply the way Reply.String does, with the value of
// every sensitive field replaced. It works on copies of the sentences' pairs:
// the reply is decoded afterwards and must keep its real values.
func redactReply(r *routeros.Reply) string {
	var sb strings.Builder

	write := func(sen *proto.Sentence) {
		copied := proto.Sentence{Word: sen.Word, Tag: sen.Tag, List: make([]proto.Pair, len(sen.List))}
		for i, pair := range sen.List {
			if pair.Value != "" && isSensitiveName(pair.Key) {
				pair.Value = redactedValue
			}
			copied.List[i] = pair
		}
		sb.WriteString(copied.String())
	}

	for _, sen := range r.Re {
		write(sen)
		sb.WriteRune('\n')
	}
	if r.Done != nil {
		write(r.Done)
	}

	return sb.String()
}

// redactJSON returns a REST body for logging with the value of every sensitive
// string member replaced.
func redactJSON(body []byte) string {
	loadSensitiveNames()
	return sensitiveJSONField.ReplaceAllString(string(body), `$1"`+redactedValue+`"`)
}
