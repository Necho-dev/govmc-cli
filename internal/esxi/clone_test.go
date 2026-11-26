package esxi

import (
	"context"
	"testing"
)

func TestOptionalInt(t *testing.T) {
	if v, ok := optionalInt(nil, 8); !ok || v != 8 {
		t.Fatalf("nil => default")
	}
	z := int32(0)
	if _, ok := optionalInt(&z, 8); ok {
		t.Fatal("0 means skip")
	}
}

func TestBuildClonePlanFromSpecSkipsNic(t *testing.T) {
	c := &Client{}
	plan, err := c.BuildClonePlanFromSpec(context.TODO(), CloneSpec{Src: "A", Dst: "B", Datastore: "ds", SrcFolder: "A"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Steps {
		if s.Op == "networks" {
			t.Fatal("no portgroup => no nic step")
		}
	}
}

func TestCrossStoreClonePlanTargetsEveryWrite(t *testing.T) {
	plan, err := (&Client{}).BuildClonePlanFromSpec(context.Background(), CloneSpec{Src: "base", Dst: "child", Datastore: "source", DestinationDatastore: "target", SrcFolder: "base"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.SourceDatastore != "source" || plan.DestinationDatastore != "target" {
		t.Fatal(plan)
	}
	for _, step := range plan.Steps {
		switch step.Op {
		case "copy":
			if step.Datastore != "source" || step.DestinationDatastore != "target" {
				t.Fatal(step)
			}
		case "patch-vmdk", "patch-vmx", "register":
			if step.Datastore != "target" {
				t.Fatal(step)
			}
		}
	}
}
