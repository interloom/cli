package cmd

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"

	"github.com/interloom/cli/internal/api"
	"github.com/interloom/cli/internal/client"
	"github.com/spf13/cobra"
)

const (
	keyGroupBy      = "group_by"
	usageSortCost   = "cost"
	usageSortTokens = "tokens"
)

// usageSortFields maps --sort values to the usage totals field they order by.
var usageSortFields = map[string]string{
	usageSortCost:   "total_cost_amount",
	usageSortTokens: "total_tokens",
}

func newCasesCmd() *cobra.Command {
	cmd := newResourceCmd(apiResource(resourceCases))
	cmd.AddCommand(newUsageCmd(resourceCases))
	return cmd
}

func newUsageCmd(resourceName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "usage <id>",
		Short: "Get all-time usage totals",
		Long: "Get all-time usage totals. Metrics can arrive late, and costs in EUR can be incomplete or null.\n" +
			"Case totals include descendants. Space usage requires owner or manager access.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			raw, err := getUsage(cmd.Context(), c, resourceName, args[0])
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	if resourceName == resourceSpaces {
		cmd.AddCommand(newUsageBreakdownsCmd())
	}
	return cmd
}

func newUsageBreakdownsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "breakdowns <space-id>",
		Short: "Get all-time Space usage grouped by case, model, or agent",
		Long: "Get all-time Space usage groups in group-key order. Requires owner or manager access.\n" +
			"Case groups can have different totals from Space usage. Omit --limit for all groups.\n" +
			"--sort cost|tokens orders groups highest first, with null values last. It fetches every\n" +
			"group and applies --limit after sorting.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			group, _ := cmd.Flags().GetString("group-by")
			if !api.ListSpaceUsageBreakdownsParamsGroupBy(group).Valid() {
				return fmt.Errorf("--group-by must be case, model, or agent")
			}
			sortBy, _ := cmd.Flags().GetString(keySort)
			if _, ok := usageSortFields[sortBy]; sortBy != "" && !ok {
				return fmt.Errorf("--sort must be cost or tokens")
			}
			limit := 0
			if cmd.Flags().Changed(argLimit) {
				limit, _ = cmd.Flags().GetInt(argLimit)
				if limit < 1 {
					return fmt.Errorf("--limit must be at least 1")
				}
			}
			c, err := newClient()
			if err != nil {
				return err
			}
			raw, err := listUsageBreakdowns(cmd.Context(), c, args[0], group, sortBy, limit)
			if err != nil {
				return err
			}
			return printResult(raw)
		},
	}
	cmd.Flags().String("group-by", "", "group by case, model, or agent (required)")
	cmd.Flags().Int(argLimit, 0, "maximum groups to return (at least 1); omit for all groups")
	cmd.Flags().String(keySort, "", "order groups by cost or tokens, highest first; omit for group-key order")
	return cmd
}

func getUsage(ctx context.Context, c *client.Client, resourceName, id string) (json.RawMessage, error) {
	return c.List(ctx, resourceName+"/"+url.PathEscape(id)+"/usage", nil)
}

// listUsageBreakdowns fetches Space usage groups. The API applies limit in
// group-key order, so a sorted request fetches every group and limits after
// sorting. A limit of 0 returns all groups.
func listUsageBreakdowns(ctx context.Context, c *client.Client, spaceID, group, sortBy string, limit int) (json.RawMessage, error) {
	q := url.Values{keyGroupBy: {group}}
	if limit > 0 && sortBy == "" {
		q.Set(argLimit, fmt.Sprint(limit))
	}
	path := resourceSpaces + "/" + url.PathEscape(spaceID) + "/usage/breakdowns"
	raw, err := c.List(ctx, path, q)
	if err != nil || sortBy == "" {
		return raw, err
	}
	return sortUsageBreakdowns(raw, usageSortFields[sortBy], limit)
}

// sortUsageBreakdowns orders the response's data groups by a totals field,
// highest first. Null or missing values sort last and ties keep the API's
// group-key order. Each group's JSON is kept as returned; only the order changes.
func sortUsageBreakdowns(raw json.RawMessage, field string, limit int) (json.RawMessage, error) {
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode usage breakdowns: %w", err)
	}
	var groups []json.RawMessage
	if err := json.Unmarshal(resp["data"], &groups); err != nil {
		return nil, fmt.Errorf("decode usage breakdowns: %w", err)
	}
	type keyedGroup struct {
		raw   json.RawMessage
		value *float64
	}
	items := make([]keyedGroup, len(groups))
	for i, g := range groups {
		var group struct {
			Totals map[string]*float64 `json:"totals"`
		}
		if err := json.Unmarshal(g, &group); err != nil {
			return nil, fmt.Errorf("decode usage breakdown group: %w", err)
		}
		items[i] = keyedGroup{raw: g, value: group.Totals[field]}
	}
	slices.SortStableFunc(items, func(a, b keyedGroup) int { return compareUsageDesc(a.value, b.value) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	sorted := make([]json.RawMessage, len(items))
	for i, item := range items {
		sorted[i] = item.raw
	}
	data, err := json.Marshal(sorted)
	if err != nil {
		return nil, err
	}
	resp["data"] = data
	return json.Marshal(resp)
}

// compareUsageDesc orders usage values highest first, with nulls last.
func compareUsageDesc(a, b *float64) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	}
	return cmp.Compare(*b, *a)
}
