package workspace

import "testing"

func TestJumpReferencesCyclesAndDeletion(t *testing.T) {
	s := Empty()
	a := NormalizeHost(Host{ID: "a", Name: "A", Address: "127.0.0.1", Port: 22, Username: "user", Group: Ungrouped, Authentication: "password"})
	b := a
	b.ID = "b"
	b.Name = "B"
	b.JumpHostIDs = []string{"a"}
	s.Hosts = []Host{a, b}
	if err := Validate(s, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Next(s, Change{Kind: "deleteHost", ID: "a"}, nil); err == nil {
		t.Fatal("deleted a referenced jump")
	}
	cycle := a
	cycle.JumpHostIDs = []string{"b"}
	if _, _, err := Next(s, Change{Kind: "saveHost", Host: cycle}, nil); err == nil {
		t.Fatal("accepted a cycle")
	}
	b.JumpHostIDs = []string{"missing"}
	if ValidateNetworks([]Host{a, b}) == nil {
		t.Fatal("accepted missing jump")
	}
	b.JumpHostIDs = []string{"a", "a"}
	if ValidateNetworks([]Host{a, b}) == nil {
		t.Fatal("accepted repeated jump")
	}
}

func TestForwardValidationAndResourceBounds(t *testing.T) {
	r := ForwardRule{ID: "db", Name: "Database", Kind: "local", ListenAddress: "127.0.0.1", ListenPort: 15432, TargetAddress: "db.internal", TargetPort: 5432, Enabled: true}
	if e := ValidateForwards([]ForwardRule{r}); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*ForwardRule){func(x *ForwardRule) { x.ListenAddress = "0.0.0.999" }, func(x *ForwardRule) { x.TargetPort = 65536 }, func(x *ForwardRule) { x.Kind = "shell" }, func(x *ForwardRule) { x.ID = "" }} {
		copy := r
		change(&copy)
		if ValidateForwards([]ForwardRule{copy}) == nil {
			t.Fatal("invalid rule accepted", copy)
		}
	}
	d := r
	d.ID = "socks"
	d.Kind = "dynamic"
	d.TargetAddress = ""
	d.TargetPort = 0
	if ValidateForwards([]ForwardRule{r, d}) == nil {
		t.Fatal("local and dynamic listeners collided")
	}
	d.Kind = "remote"
	d.TargetAddress = "127.0.0.1"
	d.TargetPort = 5432
	if e := ValidateForwards([]ForwardRule{r, d}); e != nil {
		t.Fatal("separate listener namespaces rejected", e)
	}
	rules := make([]ForwardRule, 33)
	if ValidateForwards(rules) == nil {
		t.Fatal("unbounded rules accepted")
	}
}
