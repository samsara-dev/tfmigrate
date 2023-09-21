package tfmigrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/minamijoyo/tfmigrate/tfexec"
)

func TestStateMigratorConfigNewMigrator(t *testing.T) {
	cases := []struct {
		desc   string
		config *StateMigratorConfig
		o      *MigratorOption
		ok     bool
	}{
		{
			desc: "valid (with dir)",
			config: &StateMigratorConfig{
				Dir: "dir1",
				Actions: []string{
					"mv null_resource.foo null_resource.foo2",
					"mv null_resource.bar null_resource.bar2",
					"rm time_static.baz",
					"import time_static.qux 2006-01-02T15:04:05Z",
				},
			},
			o: &MigratorOption{
				ExecPath: "direnv exec . terraform",
			},
			ok: true,
		},
		{
			desc: "valid (without dir)",
			config: &StateMigratorConfig{
				Dir: "",
				Actions: []string{
					"mv null_resource.foo null_resource.foo2",
					"mv null_resource.bar null_resource.bar2",
					"rm time_static.baz",
					"import time_static.qux 2006-01-02T15:04:05Z",
				},
			},
			o: &MigratorOption{
				ExecPath: "direnv exec . terraform",
			},
			ok: true,
		},
		{
			desc: "valid in non-default workspace",
			config: &StateMigratorConfig{
				Dir: "dir1",
				Actions: []string{
					"mv null_resource.foo null_resource.foo2",
					"mv null_resource.bar null_resource.bar2",
					"rm time_static.baz",
					"import time_static.qux 2006-01-02T15:04:05Z",
				},
				Workspace: "workspace1",
			},
			o: &MigratorOption{
				ExecPath: "direnv exec . terraform",
			},
			ok: true,
		},
		{
			desc: "invalid action",
			config: &StateMigratorConfig{
				Dir: "",
				Actions: []string{
					"mv null_resource.foo",
				},
			},
			o:  nil,
			ok: false,
		},
		{
			desc: "no actions",
			config: &StateMigratorConfig{
				Dir:     "",
				Actions: []string{},
			},
			o:  nil,
			ok: false,
		},
		{
			desc: "with force true",
			config: &StateMigratorConfig{
				Dir: "dir1",
				Actions: []string{
					"mv null_resource.foo null_resource.foo2",
					"mv null_resource.bar null_resource.bar2",
					"rm time_static.baz",
					"import time_static.qux 2006-01-02T15:04:05Z",
				},
				Force: true,
			},
			o:  nil,
			ok: true,
		},
		{
			desc: "with skip_plan true",
			config: &StateMigratorConfig{
				Dir: "dir1",
				Actions: []string{
					"mv null_resource.foo null_resource.foo2",
					"mv null_resource.bar null_resource.bar2",
					"rm time_static.baz",
					"import time_static.qux 2006-01-02T15:04:05Z",
				},
				SkipPlan: true,
			},
			o:  nil,
			ok: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := tc.config.NewMigrator(tc.o)
			if tc.ok && err != nil {
				t.Fatalf("unexpected err: %s", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("expected to return an error, but no error, got: %#v", got)
			}
			if tc.ok {
				_ = got.(*StateMigrator)
			}
		})
	}
}

func TestAccStateMigratorApplySimple(t *testing.T) {
	tfexec.SkipUnlessAcceptanceTestEnabled(t)

	backend := tfexec.GetTestAccBackendS3Config(t.Name())

	source := `
resource "null_resource" "foo" {}
resource "null_resource" "bar" {}
resource "null_resource" "baz" {}
resource "time_static" "qux" { triggers = {} }
`

	workspace := "default"
	tf := tfexec.SetupTestAccWithApply(t, workspace, backend+source)
	ctx := context.Background()

	updatedSource := `
resource "null_resource" "foo2" {}
resource "null_resource" "baz" {}
resource "time_static" "qux" { triggers = {} }
`

	tfexec.UpdateTestAccSource(t, tf, backend+updatedSource)

	_, err := tf.StateRm(ctx, nil, []string{"time_static.qux"})
	if err != nil {
		t.Fatalf("failed to run terraform state rm: %s", err)
	}

	changed, err := tf.PlanHasChange(ctx, nil)
	if err != nil {
		t.Fatalf("failed to run PlanHasChange: %s", err)
	}
	if !changed {
		t.Fatalf("expect to have changes")
	}

	actions := []StateAction{
		NewStateMvAction("null_resource.foo", "null_resource.foo2"),
		NewStateRmAction([]string{"null_resource.bar"}),
		NewStateImportAction("time_static.qux", "2006-01-02T15:04:05Z"),
	}

	force := false
	m := NewStateMigrator(tf.Dir(), workspace, actions, &MigratorOption{}, force, false)
	err = m.Plan(ctx)
	if err != nil {
		t.Fatalf("failed to run migrator plan: %s", err)
	}

	err = m.Apply(ctx)
	if err != nil {
		t.Fatalf("failed to run migrator apply: %s", err)
	}

	got, err := tf.StateList(ctx, nil, nil)
	if err != nil {
		t.Fatalf("failed to run terraform state list: %s", err)
	}

	want := []string{
		"null_resource.foo2",
		"null_resource.baz",
		"time_static.qux",
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got state: %v, want state: %v", got, want)
	}

	changed, err = tf.PlanHasChange(ctx, nil)
	if err != nil {
		t.Fatalf("failed to run PlanHasChange: %s", err)
	}
	if changed {
		t.Fatalf("expect not to have changes")
	}
}

func TestAccStateMigratorApplyWithWorkspace(t *testing.T) {
	tfexec.SkipUnlessAcceptanceTestEnabled(t)

	backend := tfexec.GetTestAccBackendS3Config(t.Name())

	source := `
resource "null_resource" "foo" {}
resource "null_resource" "bar" {}
`

	workspace := "workspace1"
	tf := tfexec.SetupTestAccWithApply(t, workspace, backend+source)
	ctx := context.Background()

	updatedSource := `
resource "null_resource" "foo2" {}
resource "null_resource" "bar" {}
`

	tfexec.UpdateTestAccSource(t, tf, backend+updatedSource)

	changed, err := tf.PlanHasChange(ctx, nil)
	if err != nil {
		t.Fatalf("failed to run PlanHasChange: %s", err)
	}
	if !changed {
		t.Fatalf("expect to have changes")
	}

	actions := []StateAction{
		NewStateMvAction("null_resource.foo", "null_resource.foo2"),
	}

	force := false
	m := NewStateMigrator(tf.Dir(), workspace, actions, &MigratorOption{}, force, false)
	err = m.Plan(ctx)
	if err != nil {
		t.Fatalf("failed to run migrator plan: %s", err)
	}

	err = m.Apply(ctx)
	if err != nil {
		t.Fatalf("failed to run migrator apply: %s", err)
	}

	got, err := tf.StateList(ctx, nil, nil)
	if err != nil {
		t.Fatalf("failed to run terraform state list: %s", err)
	}

	want := []string{
		"null_resource.foo2",
		"null_resource.bar",
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got state: %v, want state: %v", got, want)
	}

	changed, err = tf.PlanHasChange(ctx, nil)
	if err != nil {
		t.Fatalf("failed to run PlanHasChange: %s", err)
	}
	if changed {
		t.Fatalf("expect not to have changes")
	}
}

func TestAccStateMigratorApplyWithForce(t *testing.T) {
	tfexec.SkipUnlessAcceptanceTestEnabled(t)

	backend := tfexec.GetTestAccBackendS3Config(t.Name())

	source := `
resource "null_resource" "foo" {}
resource "null_resource" "bar" {}
`

	workspace := "default"
	tf := tfexec.SetupTestAccWithApply(t, workspace, backend+source)
	ctx := context.Background()

	updatedSource := `
resource "null_resource" "foo2" {}
resource "null_resource" "bar" {}
resource "null_resource" "baz" {}
`

	tfexec.UpdateTestAccSource(t, tf, backend+updatedSource)

	changed, err := tf.PlanHasChange(ctx, nil)
	if err != nil {
		t.Fatalf("failed to run PlanHasChange: %s", err)
	}
	if !changed {
		t.Fatalf("expect to have changes")
	}

	actions := []StateAction{
		NewStateMvAction("null_resource.foo", "null_resource.foo2"),
	}

	o := &MigratorOption{}
	o.PlanOut = "foo.tfplan"
	force := true
	m := NewStateMigrator(tf.Dir(), workspace, actions, o, force, false)
	err = m.Plan(ctx)
	if err != nil {
		t.Fatalf("failed to run migrator plan: %s", err)
	}

	err = m.Apply(ctx)
	if err != nil {
		t.Fatalf("failed to run migrator apply: %s", err)
	}

	got, err := tf.StateList(ctx, nil, nil)
	if err != nil {
		t.Fatalf("failed to run terraform state list: %s", err)
	}

	want := []string{
		"null_resource.foo2",
		"null_resource.bar",
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got state: %v, want state: %v", got, want)
	}

	changed, err = tf.PlanHasChange(ctx, nil)
	if err != nil {
		t.Fatalf("failed to run PlanHasChange: %s", err)
	}
	if !changed {
		t.Fatalf("expect to have changes")
	}

	// A pre-release can only be compared between pre-releases due to the
	// limitations of the hashicorp/go-version libraries and will not behave as
	// expected, so skip the following test.
	// https://github.com/hashicorp/go-version/pull/35
	tfVersioPreRelease, err := tfexec.IsPreleaseTerraformVersion(ctx, tf)
	if err != nil {
		t.Fatalf("failed to check if terraform version is pre-release: %s", err)
	}
	if tfVersioPreRelease {
		t.Skip("skip the following test because a pre-release can only be compared between pre-releases")
	}

	// Note that the saved plan file is not applicable in Terraform 1.1+.
	// https://github.com/minamijoyo/tfmigrate/pull/63
	// It's intended to use only for static analysis.
	// https://github.com/minamijoyo/tfmigrate/issues/106
	tfVersionMatched, err := tfexec.MatchTerraformVersion(ctx, tf, ">= 1.1.0")
	if err != nil {
		t.Fatalf("failed to check terraform version constraints: %s", err)
	}
	if tfVersionMatched {
		t.Skip("skip the following test because the saved plan can't apply in Terraform v1.1+")
	}

	// apply the saved plan files
	plan, err := os.ReadFile(filepath.Join(tf.Dir(), o.PlanOut))
	if err != nil {
		t.Fatalf("failed to read a saved plan file: %s", err)
	}
	err = tf.Apply(ctx, tfexec.NewPlan(plan), "-input=false", "-no-color")
	if err != nil {
		t.Fatalf("failed to apply the saved plan file: %s", err)
	}

	// Terraform >= v0.12.25 and < v0.13 has a bug for state push -force
	// https://github.com/hashicorp/terraform/issues/25761
	tfVersionMatched, err = tfexec.MatchTerraformVersion(ctx, tf, ">= 0.12.25, < 0.13")
	if err != nil {
		t.Fatalf("failed to check terraform version constraints: %s", err)
	}
	if tfVersionMatched {
		t.Skip("skip the following test due to a bug in Terraform v0.12")
	}

	// Note that applying the plan file only affects a local state,
	// make sure to force push it to remote after terraform apply.
	// The -force flag is required here because the lineage of the state was changed.
	state, err := os.ReadFile(filepath.Join(tf.Dir(), "terraform.tfstate"))
	if err != nil {
		t.Fatalf("failed to read a local state file: %s", err)
	}
	err = tf.StatePush(ctx, tfexec.NewState(state), "-force")
	if err != nil {
		t.Fatalf("failed to force push the local state: %s", err)
	}

	// confirm no changes
	changed, err = tf.PlanHasChange(ctx, nil)
	if err != nil {
		t.Fatalf("failed to run PlanHasChange: %s", err)
	}
	if changed {
		t.Fatalf("expect not to have changes")
	}
}

func TestAccStateMigratorApplyWithSkipPlan(t *testing.T) {
	tfexec.SkipUnlessAcceptanceTestEnabled(t)

	backend := tfexec.GetTestAccBackendS3Config(t.Name())

	source := `
resource "null_resource" "foo" {}
resource "null_resource" "bar" {}
`

	workspace := "default"
	tf := tfexec.SetupTestAccWithApply(t, workspace, backend+source)
	ctx := context.Background()

	updatedSource := source

	tfexec.UpdateTestAccSource(t, tf, backend+updatedSource)

	changed, err := tf.PlanHasChange(ctx, nil)
	if err != nil {
		t.Fatalf("failed to run PlanHasChange: %s", err)
	}
	if changed {
		t.Fatalf("expect not to have changes")
	}

	actions := []StateAction{
		NewStateMvAction("null_resource.foo", "null_resource.foo2"),
	}

	force := false
	skipPlan := true
	m := NewStateMigrator(tf.Dir(), workspace, actions, &MigratorOption{}, force, skipPlan)
	err = m.Plan(ctx)
	if err != nil {
		t.Fatalf("failed to run migrator plan: %s", err)
	}

	err = m.Apply(ctx)
	if err != nil {
		t.Fatalf("failed to run migrator apply: %s", err)
	}

	got, err := tf.StateList(ctx, nil, nil)
	if err != nil {
		t.Fatalf("failed to run terraform state list: %s", err)
	}

	want := []string{
		"null_resource.foo2",
		"null_resource.bar",
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got state: %v, want state: %v", got, want)
	}

	changed, err = tf.PlanHasChange(ctx, nil)
	if err != nil {
		t.Fatalf("failed to run PlanHasChange: %s", err)
	}
	if !changed {
		t.Fatalf("expect to have changes")
	}
}

func TestAccStateMigratorPlanWithSwitchBackToRemoteFuncError(t *testing.T) {
	tfexec.SkipUnlessAcceptanceTestEnabled(t)

	backend := tfexec.GetTestAccBackendS3Config(t.Name())

	// Intentionally remove the bucket key from Terraform backend configuration,
	// to force an error when switching back to remote state.
	backend = strings.ReplaceAll(backend, fmt.Sprintf("bucket = \"%s\"", tfexec.TestS3Bucket), "")

	source := `
resource "null_resource" "foo" {}
resource "null_resource" "bar" {}
`

	workspace := "default"
	// Explicitly pass a -backend-config=bucket= option to terraform init, such
	// that the initial init/apply works.
	tf := tfexec.SetupTestAccWithApply(t, workspace, backend+source, fmt.Sprintf("-backend-config=bucket=%s", tfexec.TestS3Bucket))
	ctx := context.Background()

	updatedSource := `
resource "null_resource" "foo2" {}
resource "null_resource" "bar" {}
`

	tfexec.UpdateTestAccSource(t, tf, backend+updatedSource)

	actions := []StateAction{
		NewStateMvAction("null_resource.foo", "null_resource.foo2"),
	}

	force := false
	m := NewStateMigrator(tf.Dir(), workspace, actions, &MigratorOption{}, force, false)

	err := m.Plan(ctx)
	if err == nil {
		t.Fatalf("expected migrator plan error")
	}

	expected := "Error: \"bucket\": required field is not set"
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected migrator plan error to contain %s, got: %s", expected, err.Error())
	}
}

func TestAccStateMigratorPlanWithInvalidMigration(t *testing.T) {
	tfexec.SkipUnlessAcceptanceTestEnabled(t)

	backend := tfexec.GetTestAccBackendS3Config(t.Name())

	source := `
resource "null_resource" "foo" {}
resource "null_resource" "bar" {}
`

	workspace := "default"
	tf := tfexec.SetupTestAccWithApply(t, workspace, backend+source)
	ctx := context.Background()

	updatedSource := `
resource "null_resource" "foo2" {}
resource "null_resource" "bar" {}
`

	tfexec.UpdateTestAccSource(t, tf, backend+updatedSource)

	actions := []StateAction{
		NewStateMvAction("null_resource.doesnotexist", "null_resource.foo2"),
	}

	force := false
	m := NewStateMigrator(tf.Dir(), workspace, actions, &MigratorOption{}, force, false)

	err := m.Plan(ctx)
	if err == nil {
		t.Fatalf("expected migrator plan error")
	}

	expected := "Invalid source address"
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected migrator plan error to contain %s, got: %s", expected, err.Error())
	}
}

func TestAccStateMigratorPlanWithInvalidMigrationAndSwitchBackToRemoteFuncError(t *testing.T) {
	tfexec.SkipUnlessAcceptanceTestEnabled(t)

	backend := tfexec.GetTestAccBackendS3Config(t.Name())

	// Intentionally remove the bucket key from Terraform backend configuration,
	// to force an error when switching back to remote state.
	backend = strings.ReplaceAll(backend, fmt.Sprintf("bucket = \"%s\"", tfexec.TestS3Bucket), "")

	source := `
resource "null_resource" "foo" {}
resource "null_resource" "bar" {}
`

	workspace := "default"
	// Explicitly pass a -backend-config=bucket= option to terraform init, such
	// that the initial init/apply works.
	tf := tfexec.SetupTestAccWithApply(t, workspace, backend+source, fmt.Sprintf("-backend-config=bucket=%s", tfexec.TestS3Bucket))
	ctx := context.Background()

	updatedSource := `
resource "null_resource" "foo2" {}
resource "null_resource" "bar" {}
`

	tfexec.UpdateTestAccSource(t, tf, backend+updatedSource)

	actions := []StateAction{
		NewStateMvAction("null_resource.doesnotexist", "null_resource.foo2"),
	}

	force := false
	m := NewStateMigrator(tf.Dir(), workspace, actions, &MigratorOption{}, force, false)

	err := m.Plan(ctx)
	if err == nil {
		t.Fatalf("expected migrator plan error")
	}

	expected := "Invalid source address"
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected migrator plan error to contain %s, got: %s", expected, err.Error())
	}

	expected = "Error: \"bucket\": required field is not set"
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected migrator plan error to contain %s, got: %s", expected, err.Error())
	}
}
