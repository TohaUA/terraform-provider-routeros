package routeros

import (
	"strings"
	"testing"

	"github.com/go-routeros/routeros/v3"
	"github.com/go-routeros/routeros/v3/proto"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// Every Sensitive field in the provider has to be in the redaction set, nested
// blocks included, or its value is logged in clear.
func TestSensitiveNamesCoverEverySensitiveField(t *testing.T) {
	loadSensitiveNames()
	if len(sensitiveNames) == 0 {
		t.Fatal("no sensitive field names were collected")
	}

	var walk func(owner string, s map[string]*schema.Schema)
	walk = func(owner string, s map[string]*schema.Schema) {
		for name, field := range s {
			if field.Sensitive {
				if _, ok := sensitiveNames[SnakeToKebab(name)]; !ok {
					t.Errorf("%s.%s is Sensitive but would be logged in clear", owner, name)
				}
			}
			if elem, ok := field.Elem.(*schema.Resource); ok {
				walk(owner+"."+name, elem.Schema)
			}
		}
	}

	p := Provider()
	for name, r := range p.ResourcesMap {
		walk(name, r.Schema)
	}
	for name, r := range p.DataSourcesMap {
		walk(name, r.Schema)
	}
}

func TestRedactAPIWords(t *testing.T) {
	words := []string{
		"/ppp/secret/add",
		"=name=alice",
		"=password=hunter2",
		"=private-key=abc=def=",  // a value containing '=' is redacted whole
		"=!password=",            // an unset has no value to hide
		"=input.auth-key=k3y",    // a sensitive name inside a nested block
		"=comment=password=nope", // a sensitive word in a value is not a key
		"?.id=*1",                // queries are not =name=value words
	}
	sent := append([]string(nil), words...)

	logged := strings.Join(redactAPIWords(words), " ")

	for _, secret := range []string{"hunter2", "abc=def=", "k3y"} {
		if strings.Contains(logged, secret) {
			t.Errorf("the log still carries %q: %s", secret, logged)
		}
	}
	for _, kept := range []string{"/ppp/secret/add", "=name=alice", "=password=***", "=private-key=***",
		"=!password= ", "=input.auth-key=***", "=comment=password=nope", "?.id=*1"} {
		if !strings.Contains(logged+" ", kept) {
			t.Errorf("the log lost %q: %s", kept, logged)
		}
	}

	for i := range words {
		if words[i] != sent[i] {
			t.Fatalf("redacting the log changed word %d of the command sent to the router: %q -> %q", i, sent[i], words[i])
		}
	}
}

func TestRedactReply(t *testing.T) {
	reply := &routeros.Reply{
		Re: []*proto.Sentence{{
			Word: "!re",
			List: []proto.Pair{{Key: ".id", Value: "*1"}, {Key: "name", Value: "alice"}, {Key: "password", Value: "hunter2"}},
			Map:  map[string]string{".id": "*1", "name": "alice", "password": "hunter2"},
		}},
		Done: &proto.Sentence{Word: "!done"},
	}

	logged := redactReply(reply)

	if strings.Contains(logged, "hunter2") {
		t.Errorf("the logged reply still carries the password: %s", logged)
	}
	if !strings.Contains(logged, "alice") || !strings.Contains(logged, "!done") {
		t.Errorf("the logged reply lost non-sensitive content: %s", logged)
	}
	if reply.Re[0].List[2].Value != "hunter2" || reply.Re[0].Map["password"] != "hunter2" {
		t.Fatal("redacting the log changed the reply the provider decodes")
	}
}

func TestRedactJSON(t *testing.T) {
	body := []byte(`[{".id":"*1","name":"alice","password":"hun\"ter2","private-key":"abc",` +
		`"comment":"the password is not here","!password":"","input.auth-key":"k3y"}]`)
	original := string(body)

	logged := redactJSON(body)

	for _, secret := range []string{`hun\"ter2`, `"abc"`, `k3y`} {
		if strings.Contains(logged, secret) {
			t.Errorf("the log still carries %s: %s", secret, logged)
		}
	}
	for _, kept := range []string{`"name":"alice"`, `"password":"***"`, `"private-key":"***"`,
		`"comment":"the password is not here"`, `"!password":""`, `"input.auth-key":"***"`} {
		if !strings.Contains(logged, kept) {
			t.Errorf("the log lost %s: %s", kept, logged)
		}
	}
	if string(body) != original {
		t.Fatal("redacting the log changed the body that is decoded")
	}
}
