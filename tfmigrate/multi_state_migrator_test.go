package tfmigrate

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/minamijoyo/tfmigrate/tfexec"
)

func TestAccMultiStateMigratorApply(t *testing.T) {
	tfexec.SkipUnlessAcceptanceTestEnabled(t)
	ctx := context.Background()

	fromBackend := tfexec.GetTestAccBackendS3Config(t.Name() + "/fromDir")
	fromSource := `
resource "aws_security_group" "foo" {}
resource "aws_security_group" "bar" {}
resource "aws_security_group" "baz" {}
`
	fromTf := tfexec.SetupTestAccWithApply(t, fromBackend+fromSource)
	toBackend := tfexec.GetTestAccBackendS3Config(t.Name() + "/toDir")
	toSource := `
resource "aws_security_group" "qux" {}
`
	toTf := tfexec.SetupTestAccWithApply(t, toBackend+toSource)

	fromUpdatedSource := `
resource "aws_security_group" "baz" {}
`
	tfexec.UpdateTestAccSource(t, fromTf, fromBackend+fromUpdatedSource)
	toUpdatedSource := `
resource "aws_security_group" "foo" {}
resource "aws_security_group" "bar2" {}
resource "aws_security_group" "qux" {}
`
	tfexec.UpdateTestAccSource(t, toTf, toBackend+toUpdatedSource)

	changed, err := fromTf.PlanHasChange(ctx, nil, "")
	if err != nil {
		t.Fatalf("failed to run PlanHasChange in fromDir: %s", err)
	}
	if !changed {
		t.Fatalf("expect to have changes in fromDir")
	}
	changed, err = toTf.PlanHasChange(ctx, nil, "")
	if err != nil {
		t.Fatalf("failed to run PlanHasChange in toDir: %s", err)
	}
	if !changed {
		t.Fatalf("expect to have changes in toDir")
	}

	actions := []MultiStateAction{
		NewMultiStateMvAction("aws_security_group.foo", "aws_security_group.foo"),
		NewMultiStateMvAction("aws_security_group.bar", "aws_security_group.bar2"),
	}

	m := NewMultiStateMigrator(fromTf.Dir(), toTf.Dir(), actions, nil)
	err = m.Plan(ctx)
	if err != nil {
		t.Fatalf("failed to run migrator plan: %s", err)
	}

	err = m.Apply(ctx)
	if err != nil {
		t.Fatalf("failed to run migrator apply: %s", err)
	}

	fromGot, err := fromTf.StateList(ctx, nil, nil)
	if err != nil {
		t.Fatalf("failed to run terraform state list in fromDir: %s", err)
	}
	fromWant := []string{
		"aws_security_group.baz",
	}
	sort.Strings(fromGot)
	sort.Strings(fromWant)
	if !reflect.DeepEqual(fromGot, fromWant) {
		t.Errorf("got state: %v, want state: %v in fromDir", fromGot, fromWant)
	}
	toGot, err := toTf.StateList(ctx, nil, nil)
	if err != nil {
		t.Fatalf("failed to run terraform state list in toDir: %s", err)
	}
	toWant := []string{
		"aws_security_group.foo",
		"aws_security_group.bar2",
		"aws_security_group.qux",
	}
	sort.Strings(toGot)
	sort.Strings(toWant)
	if !reflect.DeepEqual(toGot, toWant) {
		t.Errorf("got state: %v, want state: %v in toDir", toGot, toWant)
	}

	changed, err = fromTf.PlanHasChange(ctx, nil, "")
	if err != nil {
		t.Fatalf("failed to run PlanHasChange in fromDir: %s", err)
	}
	if changed {
		t.Fatalf("expect not to have changes in fromDir")
	}
	changed, err = toTf.PlanHasChange(ctx, nil, "")
	if err != nil {
		t.Fatalf("failed to run PlanHasChange in toDir: %s", err)
	}
	if changed {
		t.Fatalf("expect not to have changes in toDir")
	}
}
