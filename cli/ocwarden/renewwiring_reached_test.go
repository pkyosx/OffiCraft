package main

// renewwiring_reached_test.go — the two lines that switch self-renewal on, and
// why they need a syntax check rather than a behavioural one.
//
// 🔴 THE FEATURE IS ONE CALL AWAY FROM OFF, AND NO TEST CAN REACH THAT CALL.
// newSelfUpdater opens with refuseInTestBinary, so nothing in this package can
// construct it — which means `u.apply(newRenewalWiring(cfg, env))` inside it, and
// the `newSelfUpdater(cfg, env, logf)` that calls it, are both unobservable from
// any test that runs. Review measured what that buys an attacker, or a careless
// edit:
//
//	drop the u.apply(...)          ⇒ go test ./... rc=0. maybeRenewCredential's
//	                                 `u.renew == nil` guard swallows everything,
//	                                 fleet-wide, with not one log line — it is not
//	                                 an error path, it is "renewal is not in play".
//	newSelfUpdater(cfg, renv, ...) ⇒ go test ./... rc=0. renv is the tokfile-folded
//	                                 view, one identifier away in the same scope;
//	                                 it reports the token FILE as if someone had
//	                                 exported OC_TOKEN, which trips the
//	                                 infinite-exec guard on every launchd warden —
//	                                 none of which sets OC_TOKEN — so the whole
//	                                 fleet stops renewing. The doc comment on
//	                                 newRenewalWiring warns about exactly this and
//	                                 nothing enforced it.
//
// ⚠️ THIS IS A SYNTAX CHECK AND IT IS THE WEAKER KIND. It cannot tell you the
// wiring WORKS — TestNewRenewalWiring_* and TestApply_* do that, and they are the
// load-bearing ones. What it tells you is that production still reaches them. It
// is here because the alternative is not a better test, it is no test: the seam
// this would need to be behavioural does not exist, and inventing one to make an
// assertion possible would be changing production shape to suit a test. Stated
// rather than dressed up.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// callArgIdents returns the identifier names passed to the first call of fnName
// found anywhere in file, or nil if there is no such call.
func callArgIdents(t *testing.T, file, fnName string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	var args []string
	var found bool
	ast.Inspect(f, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || id.Name != fnName {
			return true
		}
		found = true
		for _, a := range call.Args {
			if ai, isIdent := a.(*ast.Ident); isIdent {
				args = append(args, ai.Name)
			} else {
				args = append(args, "<not-an-identifier>")
			}
		}
		return false
	})
	if !found {
		return nil
	}
	if args == nil {
		args = []string{}
	}
	return args
}

func TestSelfUpdater_StillWiresRenewalAndFromTheRawEnvironment(t *testing.T) {
	// ① the wiring is still applied onto the updater.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "selfupdate.go", nil, 0)
	if err != nil {
		t.Fatalf("parse selfupdate.go: %v", err)
	}
	applied := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "apply" || len(call.Args) != 1 {
			return true
		}
		inner, ok := call.Args[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, isIdent := inner.Fun.(*ast.Ident); isIdent && id.Name == "newRenewalWiring" {
			applied = true
			return false
		}
		return true
	})
	if !applied {
		t.Error("selfupdate.go no longer contains `.apply(newRenewalWiring(...))`. " +
			"Without it every renewal field stays nil and maybeRenewCredential " +
			"returns false on its first line — self-renewal is off on every machine " +
			"in the fleet, silently, and no other test in this package goes red")
	}

	// ② and it is handed the RAW environment.
	args := callArgIdents(t, "main.go", "newSelfUpdater")
	if args == nil {
		t.Fatal("main.go no longer calls newSelfUpdater — nothing starts the " +
			"self-update loop, and with it self-renewal")
	}
	if len(args) < 2 {
		t.Fatalf("newSelfUpdater called with %d args: %v", len(args), args)
	}
	if args[1] != "env" {
		t.Errorf("newSelfUpdater is handed %q as its environment, want the RAW `env`. "+
			"`renv` is the tokfile-folded view and sits one identifier away in the "+
			"same scope: it answers OC_TOKEN with the token FILE's contents, which "+
			"trips the infinite-exec guard on precisely the machines that guard "+
			"protects — every launchd warden, none of which sets OC_TOKEN — and the "+
			"whole fleet stops renewing", args[1])
	}
}
