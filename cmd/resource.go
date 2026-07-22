package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// snake_case JSON/query key names shared across resources.
const (
	resourceCaseIngestions = "case-ingestions"
	resourceCases          = "cases"
	resourceModels         = "models"
	resourceSecrets        = "secrets"
	resourceSpaces         = "spaces"
	resourceTools          = "tools"

	commandNameDelete  = "delete"
	commandNameGet     = "get"
	commandNameTrigger = "trigger"
	commandNameUpdate  = "update"
	commandUseList     = "list"
	commandUseGet      = commandNameGet + " <id>"
	commandUseCreate   = "create"
	argAll             = "all"

	keyName            = "name"
	keyTitle           = "title"
	keyDescription     = "description"
	keyData            = "data"
	keyScript          = "script"
	keySecretIDs       = "secret_ids"
	keySpaceID         = "space_id"
	keyCaseID          = "case_id"
	keyParentCaseID    = "parent_case_id"
	keyThreadID        = "thread_id"
	keyAgentID         = "agent_id"
	keyAssigneeID      = "assignee_id"
	keyStatus          = "status"
	keySort            = "sort"
	keyCursor          = "cursor"
	keyDirection       = "direction"
	keyManifest        = "manifest"
	keyFileIDs         = "file_ids"
	keyToolIDs         = "tool_ids"
	keyReasoningEffort = "reasoning_effort"

	defaultUnscopedCasesSort      = "created_at"
	defaultUnscopedCasesDirection = "desc"
	defaultScopedCasesSort        = "position"
	defaultScopedCasesDirection   = "asc"
)

// filter is a query parameter exposed as a list flag. When multi is set it is a
// repeatable string-slice flag sent as repeated query params.
type filter struct {
	name  string
	usage string
	multi bool
}

func (f filter) flagName() string {
	return strings.ReplaceAll(f.name, "_", "-")
}

// Common list filters reused across resources.
var (
	filterSpaceID   = filter{name: keySpaceID, usage: "filter by Space ID"}
	filterCaseID    = filter{name: keyCaseID, usage: "filter by Case ID"}
	filterSort      = filter{name: keySort, usage: "sort field: created_at or updated_at"}
	filterDirection = filter{name: keyDirection, usage: "sort direction: asc or desc"}
)

// field is a request-body property exposed as a create/update flag. The flag is
// kebab-case; the JSON key is the snake_case name. When multi is set it is a
// repeatable string-slice flag emitted as a JSON array. required is enforced
// only on create, and only when the body is built from flags.
type field struct {
	name     string
	usage    string
	multi    bool
	onCreate bool
	onUpdate bool
	required bool
}

func (f field) flagName() string {
	return strings.ReplaceAll(f.name, "_", "-")
}

// Common body fields reused across resources.
var (
	fieldSpaceID = field{name: keySpaceID, usage: "owning Space ID", onCreate: true, onUpdate: true}
	fieldCaseID  = field{name: keyCaseID, usage: "owning Case ID", onCreate: true, onUpdate: true}
	fieldTags    = field{name: "tags", usage: "tags (repeatable)", multi: true, onCreate: true, onUpdate: true}
)

// resource describes a REST resource. Most resources share the same five verbs
// (list/get/create/update/delete); flags below handle API resources that omit a
// standard verb or cursor pagination.
type resource struct {
	name     string   // URL segment and command name, e.g. "cases"
	singular string   // e.g. "case", used in help text
	readOnly bool     // only list + get (e.g. users)
	noGet    bool     // no item GET endpoint (e.g. models)
	noCreate bool     // no generic create (e.g. files, which uses upload)
	noUpdate bool     // no PATCH endpoint (e.g. secrets)
	noDelete bool     // no DELETE endpoint (e.g. agents)
	noPaging bool     // collection list is not cursor-paginated
	filters  []filter // list query filters
	fields   []field  // create/update body fields
}

func newResourceCmd(r resource) *cobra.Command {
	cmd := &cobra.Command{
		Use:   r.name,
		Short: fmt.Sprintf("Manage %s", r.name),
	}
	addConfigNameFlag(cmd)
	cmd.AddCommand(r.listCmd())
	if !r.noGet {
		cmd.AddCommand(r.getCmd())
	}
	if r.readOnly {
		return cmd
	}
	if !r.noCreate {
		cmd.AddCommand(r.createCmd())
	}
	if !r.noUpdate {
		cmd.AddCommand(r.updateCmd())
	}
	if !r.noDelete {
		cmd.AddCommand(r.deleteCmd())
	}
	return cmd
}

// listQuery builds the query string for a list call from the paging flags and
// the resource's filters (single-value or repeatable).
func (r resource) listQuery(cmd *cobra.Command) url.Values {
	q := url.Values{}
	if !r.noPaging && cmd.Flags().Changed("limit") {
		limit, _ := cmd.Flags().GetInt("limit")
		q.Set("limit", fmt.Sprint(limit))
	}
	if !r.noPaging {
		cur, _ := cmd.Flags().GetString("cursor")
		if cur != "" {
			q.Set("cursor", cur)
		}
	}
	for _, f := range r.filters {
		flagName := f.flagName()
		if f.multi {
			vals, _ := cmd.Flags().GetStringSlice(flagName)
			for _, v := range vals {
				if v != "" {
					q.Add(f.name, v)
				}
			}
			continue
		}
		if v, _ := cmd.Flags().GetString(flagName); v != "" {
			q.Set(f.name, v)
		}
	}
	r.applyListDefaults(q)
	return q
}

// applyListDefaults makes case ordering explicit so CLI behavior does not drift
// with API defaults. Unscoped lists default to newest-created first; Space/parent
// scoped lists default to tree position order.
func (r resource) applyListDefaults(q url.Values) {
	if r.name != resourceCases {
		return
	}
	scoped := q.Get(keySpaceID) != "" || q.Get(keyParentCaseID) != ""
	if q.Get(keySort) == "" {
		if scoped {
			q.Set(keySort, defaultScopedCasesSort)
		} else {
			q.Set(keySort, defaultUnscopedCasesSort)
		}
	}
	if q.Get(keyDirection) == "" {
		q.Set(keyDirection, defaultDirectionForCasesSort(q.Get(keySort), scoped))
	}
}

func defaultDirectionForCasesSort(sort string, scoped bool) string {
	if scoped || sort == defaultScopedCasesSort {
		return defaultScopedCasesDirection
	}
	return defaultUnscopedCasesDirection
}

func (r resource) listCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   commandUseList,
		Short: fmt.Sprintf("List %s", r.name),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			q := r.listQuery(cmd)
			all := false
			if !r.noPaging {
				all, _ = cmd.Flags().GetBool(argAll)
			}
			var raw []byte
			if all {
				raw, err = c.ListAll(cmd.Context(), r.name, q)
			} else {
				raw, err = c.List(cmd.Context(), r.name, q)
			}
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	if !r.noPaging {
		cmd.Flags().Int("limit", 0, "maximum number of items to return")
		cmd.Flags().String("cursor", "", "pagination cursor from a previous next_cursor")
		cmd.Flags().Bool(argAll, false, "fetch all pages and aggregate into a single list")
	}
	for _, f := range r.filters {
		flagName := f.flagName()
		if f.multi {
			cmd.Flags().StringSlice(flagName, nil, f.usage)
		} else {
			cmd.Flags().String(flagName, "", f.usage)
		}
	}
	return cmd
}

func (r resource) getCmd() *cobra.Command {
	return &cobra.Command{
		Use:   commandUseGet,
		Short: fmt.Sprintf("Get a single %s by ID", r.singular),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			raw, err := c.Get(cmd.Context(), r.name, args[0])
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
}

func (r resource) createCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   commandUseCreate,
		Short: fmt.Sprintf("Create a %s from a JSON body", r.singular),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			body, err := r.body(cmd, true)
			if err != nil {
				return err
			}
			raw, err := c.Create(cmd.Context(), r.name, body)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	r.addFieldFlags(cmd, true)
	addBodyFlags(cmd)
	return cmd
}

func (r resource) updateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: fmt.Sprintf("Update a %s from a JSON body", r.singular),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			body, err := r.body(cmd, false)
			if err != nil {
				return err
			}
			raw, err := c.Update(cmd.Context(), r.name, args[0], body)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	r.addFieldFlags(cmd, false)
	addBodyFlags(cmd)
	return cmd
}

func (r resource) deleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   commandNameDelete + " <id>",
		Short: fmt.Sprintf("Delete a %s by ID", r.singular),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			raw, err := c.Delete(cmd.Context(), r.name, args[0])
			if err != nil {
				return err
			}
			if len(raw) == 0 {
				raw = []byte(fmt.Sprintf(`{"id":%q,"deleted":true}`, args[0]))
			}
			return printResult(raw)
		},
	}
}

// fieldsFor returns the body fields applicable to create (or update).
func (r resource) fieldsFor(create bool) []field {
	var out []field
	for _, f := range r.fields {
		if (create && f.onCreate) || (!create && f.onUpdate) {
			out = append(out, f)
		}
	}
	return out
}

// addFieldFlags registers the per-field create/update flags for the verb.
func (r resource) addFieldFlags(cmd *cobra.Command, create bool) {
	for _, f := range r.fieldsFor(create) {
		usage := f.usage
		if create && f.required {
			usage += " (required)"
		}
		if f.multi {
			cmd.Flags().StringSlice(f.flagName(), nil, usage)
		} else {
			cmd.Flags().String(f.flagName(), "", usage)
		}
	}
}

// body resolves the request body for create/update. When any field flags are
// set it builds JSON from them (enforcing required fields on create); otherwise
// it falls back to the raw --data/--file/stdin body. Field flags and a raw body
// are mutually exclusive.
func (r resource) body(cmd *cobra.Command, create bool) ([]byte, error) {
	fields := r.fieldsFor(create)
	out := map[string]any{}
	var missing []string
	for _, f := range fields {
		flagName := f.flagName()
		if cmd.Flags().Changed(flagName) {
			if f.multi {
				vals, _ := cmd.Flags().GetStringSlice(flagName)
				out[f.name] = vals
			} else {
				v, _ := cmd.Flags().GetString(flagName)
				out[f.name] = v
			}
			continue
		}
		if create && f.required {
			missing = append(missing, "--"+flagName)
		}
	}

	if len(out) == 0 {
		return readBody(cmd)
	}
	if cmd.Flags().Changed(keyData) || cmd.Flags().Changed("file") {
		return nil, fmt.Errorf("pass either field flags or a JSON body, not both")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required flag(s): %s", strings.Join(missing, ", "))
	}
	return json.Marshal(out)
}

// addBodyFlags registers the JSON-body input flags shared by create/update.
func addBodyFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("file", "f", "", "path to a JSON body file, or - for stdin")
	cmd.Flags().StringP(keyData, "d", "", "inline JSON body")
}

// readBody resolves the request body: --data > --file > piped stdin.
func readBody(cmd *cobra.Command) ([]byte, error) {
	if data, _ := cmd.Flags().GetString(keyData); data != "" {
		return []byte(data), nil
	}
	file, _ := cmd.Flags().GetString("file")
	if file == "-" || (file == "" && !stdinIsTTY()) {
		return io.ReadAll(os.Stdin)
	}
	if file != "" {
		return os.ReadFile(file)
	}
	return nil, fmt.Errorf("no request body: pass --data, --file, or pipe JSON via stdin")
}
