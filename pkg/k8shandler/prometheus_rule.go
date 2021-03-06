package k8shandler

import (
	"context"
	"fmt"
	"os"

	"github.com/ViaQ/logerr/kverrors"
	"github.com/openshift/elasticsearch-operator/pkg/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sYAML "k8s.io/apimachinery/pkg/util/yaml"
)

const (
	alertsFilePath = "/etc/elasticsearch-operator/files/prometheus_alerts.yml"
	rulesFilePath  = "/etc/elasticsearch-operator/files/prometheus_rules.yml"
)

func (er *ElasticsearchRequest) CreateOrUpdatePrometheusRules() error {
	ctx := context.TODO()
	dpl := er.cluster

	name := fmt.Sprintf("%s-%s", dpl.Name, "prometheus-rules")

	rule, err := buildPrometheusRule(name, dpl.Namespace, dpl.Labels)
	if err != nil {
		return kverrors.Wrap(err, "failed to build prometheus rule")
	}

	dpl.AddOwnerRefTo(rule)

	err = er.client.Create(ctx, rule)
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return kverrors.Wrap(err, "failed to create prometheus rule", "rule", rule.Name)
	}

	current := &monitoringv1.PrometheusRule{}
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err = er.client.Get(ctx, types.NamespacedName{Name: rule.Name, Namespace: rule.Namespace}, current)
		if err != nil {
			return err
		}

		current.Spec = rule.Spec
		if err = er.client.Update(ctx, current); err != nil {
			return err
		}
		return nil
	})

	return kverrors.Wrap(err, "failed to update prometheus rule", "rule", rule.Name)
}

func buildPrometheusRule(ruleName string, namespace string, labels map[string]string) (*monitoringv1.PrometheusRule, error) {
	alertsRuleSpec, err := ruleSpec(utils.LookupEnvWithDefault("ALERTS_FILE_PATH", alertsFilePath))
	if err != nil {
		return nil, kverrors.Wrap(err, "failed to build rule spec")
	}
	rulesRuleSpec, err := ruleSpec(utils.LookupEnvWithDefault("RULES_FILE_PATH", rulesFilePath))
	if err != nil {
		return nil, err
	}

	alertsRuleSpec.Groups = append(alertsRuleSpec.Groups, rulesRuleSpec.Groups...)

	rule := prometheusRule(ruleName, namespace, labels)
	rule.Spec = *alertsRuleSpec

	return rule, nil
}

func prometheusRule(ruleName, namespace string, labels map[string]string) *monitoringv1.PrometheusRule {
	return &monitoringv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{
			Kind:       monitoringv1.PrometheusRuleKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      ruleName,
			Namespace: namespace,
			Labels:    labels,
		},
	}
}

func ruleSpec(filePath string) (*monitoringv1.PrometheusRuleSpec, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed to open file", "filePath", filePath)
	}
	defer f.Close()
	ruleSpec := monitoringv1.PrometheusRuleSpec{}
	if err := k8sYAML.NewYAMLOrJSONDecoder(f, 1000).Decode(&ruleSpec); err != nil {
		return nil, kverrors.Wrap(err, "failed to decode rule spec from file", "filePath", filePath)
	}
	return &ruleSpec, nil
}
