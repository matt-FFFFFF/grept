package pkg

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Azure/golden"
	"github.com/Azure/grept/pkg/githubclient"
)

var _ Data = &GitHubRepositoryRulesetsDatasource{}

type RulesetForGitHubRepositoryRulesetsDatasource struct {
	Name        string                                             `attribute:"name"`
	NodeId      string                                             `attribute:"node_id"`
	IncludeRefs []string                                           `attribute:"include_refs"`
	ExcludeRefs []string                                           `attribute:"exclude_refs"`
	Enforcement string                                             `attribute:"enforcement"`
	Rules       []RulesetForGitHubRepositoryRulesetsDatasourceRule `attribute:"rules"`
}

type RulesetForGitHubRepositoryRulesetsDatasourceRule struct {
	Type       string           `attribute:"type"`
	Parameters *json.RawMessage `attribute:"parameters"`
}

type GitHubRepositoryRulesetsDatasource struct {
	*golden.BaseBlock
	*BaseData
	Owner    string                                         `hcl:"owner"`
	RepoName string                                         `hcl:"repo_name"`
	Rulesets []RulesetForGitHubRepositoryRulesetsDatasource `attribute:"rulesets"`
}

func (g *GitHubRepositoryRulesetsDatasource) Type() string {
	return "github_repository_environments"
}

func (g *GitHubRepositoryRulesetsDatasource) ExecuteDuringPlan() error {
	client, err := githubclient.GetGithubClient()
	if err != nil {
		return fmt.Errorf("cannot create github client: %s", err.Error())
	}
	results, err := listGitHubRepositoryRulesets(client, g.Owner, g.RepoName)
	if err != nil {
		return fmt.Errorf("cannot list environments for %s/%s: %+v", g.Owner, g.RepoName, err)
	}
	g.Rulesets = results
	return nil
}

func listGitHubRepositoryRulesets(client *githubclient.Client, owner, repoName string) ([]RulesetForGitHubRepositoryRulesetsDatasource, error) {
	var results []RulesetForGitHubRepositoryRulesetsDatasource

	rulesets, _, err := client.Repositories.GetAllRulesets(context.Background(), owner, repoName, false)
	if err != nil {
		return nil, err
	}
	if rulesets == nil {
		return nil, nil
	}
	for _, ruleset := range rulesets {
		rules := make([]RulesetForGitHubRepositoryRulesetsDatasourceRule, 0)
		for _, rule := range ruleset.Rules {
			rules = append(rules, RulesetForGitHubRepositoryRulesetsDatasourceRule{
				Type:       rule.Type,
				Parameters: rule.Parameters,
			})
		}
		results = append(results, RulesetForGitHubRepositoryRulesetsDatasource{
			Name:        ruleset.Name,
			NodeId:      ruleset.GetNodeID(),
			Enforcement: ruleset.Enforcement,
			IncludeRefs: ruleset.Conditions.RefName.Include,
			ExcludeRefs: ruleset.Conditions.RefName.Exclude,
			Rules:       rules,
		})
	}
	return results, nil
}
