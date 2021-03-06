/*
Copyright (c) Facebook, Inc. and its affiliates.
All rights reserved.

This source code is licensed under the BSD-style license found in the
LICENSE file in the root directory of this source tree.
*/

package gx

import (
	"time"

	"magma/feg/gateway/policydb"
	"magma/feg/gateway/services/session_proxy/credit_control"
	"magma/lte/cloud/go/protos"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/golang/glog"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
)

var eventTriggerConversionMap = map[EventTrigger]protos.EventTrigger{
	RevalidationTimeout: protos.EventTrigger_REVALIDATION_TIMEOUT,
}

func (ccr *CreditControlRequest) FromUsageMonitorUpdate(update *protos.UsageMonitoringUpdateRequest) *CreditControlRequest {
	ccr.SessionID = update.SessionId
	ccr.RequestNumber = update.RequestNumber
	ccr.Type = credit_control.CRTUpdate
	ccr.IMSI = credit_control.RemoveIMSIPrefix(update.Sid)
	ccr.IPAddr = update.UeIpv4
	ccr.HardwareAddr = update.HardwareAddr
	ccr.UsageReports = []*UsageReport{(&UsageReport{}).FromUsageMonitorUpdate(update.Update)}
	ccr.RATType = GetRATType(update.RatType)
	ccr.IPCANType = GetIPCANType(update.RatType)
	return ccr
}

func (qos *QosRequestInfo) FromProtos(pQos *protos.QosInformationRequest) *QosRequestInfo {
	qos.ApnAggMaxBitRateDL = pQos.GetApnAmbrDl()
	qos.ApnAggMaxBitRateUL = pQos.GetApnAmbrUl()
	qos.QosClassIdentifier = pQos.GetQosClassId()
	qos.PriLevel = pQos.GetPriorityLevel()
	qos.PreCapability = pQos.GetPreemptionCapability()
	qos.PreVulnerability = pQos.GetPreemptionVulnerability()
	return qos
}

func (rd *RuleDefinition) ToProto() *protos.PolicyRule {
	monitoringKey := []byte{}
	if len(rd.MonitoringKey) > 0 {
		// no conversion needed - Monitoring-Key AVP is Octet String already
		monitoringKey = rd.MonitoringKey
	}
	var ratingGroup uint32 = 0
	if rd.RatingGroup != nil {
		ratingGroup = *rd.RatingGroup
	}
	flowList := getFlowList(rd.FlowDescriptions, rd.FlowInformations)

	var qos *protos.FlowQos
	if rd.Qos != nil {
		qos = &protos.FlowQos{}
		if rd.Qos.MaxReqBwUL != nil {
			qos.MaxReqBwUl = *rd.Qos.MaxReqBwUL
		}
		if rd.Qos.MaxReqBwDL != nil {
			qos.MaxReqBwDl = *rd.Qos.MaxReqBwDL
		}
		if rd.Qos.GbrDL != nil {
			qos.GbrDl = *rd.Qos.GbrDL
		}
		if rd.Qos.GbrUL != nil {
			qos.GbrUl = *rd.Qos.GbrUL
		}
		if rd.Qos.Qci != nil {
			qos.Qci = protos.FlowQos_Qci(*rd.Qos.Qci)
		}
	}

	return &protos.PolicyRule{
		Id:            rd.RuleName,
		RatingGroup:   ratingGroup,
		MonitoringKey: monitoringKey,
		Priority:      rd.Precedence,
		Redirect:      rd.getRedirectInfo(),
		FlowList:      flowList,
		Qos:           qos,
		TrackingType:  rd.getTrackingType(),
	}
}

func (rd *RuleDefinition) getTrackingType() protos.PolicyRule_TrackingType {
	monKeyPresent := len(rd.MonitoringKey) > 0
	if monKeyPresent && rd.RatingGroup != nil {
		return protos.PolicyRule_OCS_AND_PCRF
	} else if monKeyPresent && rd.RatingGroup == nil {
		return protos.PolicyRule_ONLY_PCRF
	} else if (!monKeyPresent) && rd.RatingGroup != nil {
		return protos.PolicyRule_ONLY_OCS
	} else {
		return protos.PolicyRule_NO_TRACKING
	}
}

func (rd *RuleDefinition) getRedirectInfo() *protos.RedirectInformation {
	if rd.RedirectInformation == nil {
		return nil
	}
	return &protos.RedirectInformation{
		Support:       protos.RedirectInformation_Support(rd.RedirectInformation.RedirectSupport),
		AddressType:   protos.RedirectInformation_AddressType(rd.RedirectInformation.RedirectAddressType),
		ServerAddress: rd.RedirectInformation.RedirectServerAddress,
	}
}

func getFlowList(flowStrings []string, flowInfos []*FlowInformation) []*protos.FlowDescription {
	allFlowStrings := flowStrings[:]
	for _, info := range flowInfos {
		allFlowStrings = append(allFlowStrings, info.FlowDescription)
	}
	var flowList []*protos.FlowDescription
	for _, flowString := range allFlowStrings {
		flow, err := policydb.GetFlowDescriptionFromFlowString(flowString)
		if err != nil {
			glog.Errorf("Could not get flow for description %s : %s", flowString, err)
		} else {
			flowList = append(flowList, flow)
		}
	}
	return flowList
}

func (rar *ReAuthRequest) ToProto(imsi, sid string, policyDBClient policydb.PolicyDBClient) *protos.PolicyReAuthRequest {
	var rulesToRemove, baseNamesToRemove []string

	for _, ruleRemove := range rar.RulesToRemove {
		rulesToRemove = append(rulesToRemove, ruleRemove.RuleNames...)
		baseNamesToRemove = append(baseNamesToRemove, ruleRemove.RuleBaseNames...)
	}

	baseNameRuleIDsToRemove := policyDBClient.GetRuleIDsForBaseNames(baseNamesToRemove)
	rulesToRemove = append(rulesToRemove, baseNameRuleIDsToRemove...)

	staticRulesToInstall, dynamicRulesToInstall := ParseRuleInstallAVPs(
		policyDBClient,
		rar.RulesToInstall,
	)

	eventTriggers, revalidationTime := GetEventTriggersRelatedInfo(rar.EventTriggers, rar.RevalidationTime)
	usageMonitoringCredits := getUsageMonitoringCredits(rar.UsageMonitors)
	qosInfo := getQoSInfo(rar.Qos)

	return &protos.PolicyReAuthRequest{
		SessionId:              sid,
		Imsi:                   imsi,
		RulesToRemove:          rulesToRemove,
		RulesToInstall:         staticRulesToInstall,
		DynamicRulesToInstall:  dynamicRulesToInstall,
		EventTriggers:          eventTriggers,
		RevalidationTime:       revalidationTime,
		UsageMonitoringCredits: usageMonitoringCredits,
		QosInfo:                qosInfo,
	}
}

func (raa *ReAuthAnswer) FromProto(sessionID string, answer *protos.PolicyReAuthAnswer) *ReAuthAnswer {
	raa.SessionID = sessionID
	raa.ResultCode = diam.Success
	raa.RuleReports = make([]*ChargingRuleReport, 0, len(answer.FailedRules))
	for ruleName, code := range answer.FailedRules {
		raa.RuleReports = append(
			raa.RuleReports,
			&ChargingRuleReport{RuleNames: []string{ruleName}, FailureCode: RuleFailureCode(code)},
		)
	}
	return raa
}

func ConvertToProtoTimestamp(unixTime *time.Time) *timestamp.Timestamp {
	if unixTime == nil {
		return nil
	}
	protoTimestamp, err := ptypes.TimestampProto(*unixTime)
	if err != nil {
		glog.Errorf("Unable to convert time.Time to google.protobuf.Timestamp: %s", err)
		return nil
	}
	return protoTimestamp
}

func ParseRuleInstallAVPs(
	policyDBClient policydb.PolicyDBClient,
	ruleInstalls []*RuleInstallAVP,
) ([]*protos.StaticRuleInstall, []*protos.DynamicRuleInstall) {
	staticRulesToInstall := make([]*protos.StaticRuleInstall, 0, len(ruleInstalls))
	dynamicRulesToInstall := make([]*protos.DynamicRuleInstall, 0, len(ruleInstalls))
	for _, ruleInstall := range ruleInstalls {
		activationTime := ConvertToProtoTimestamp(ruleInstall.RuleActivationTime)
		deactivationTime := ConvertToProtoTimestamp(ruleInstall.RuleDeactivationTime)

		for _, staticRuleName := range ruleInstall.RuleNames {
			staticRulesToInstall = append(
				staticRulesToInstall,
				&protos.StaticRuleInstall{
					RuleId:           staticRuleName,
					ActivationTime:   activationTime,
					DeactivationTime: deactivationTime,
				},
			)
		}

		if len(ruleInstall.RuleBaseNames) != 0 {
			baseNameRuleIdsToInstall := policyDBClient.GetRuleIDsForBaseNames(ruleInstall.RuleBaseNames)
			for _, baseNameRuleId := range baseNameRuleIdsToInstall {
				staticRulesToInstall = append(
					staticRulesToInstall,
					&protos.StaticRuleInstall{
						RuleId:           baseNameRuleId,
						ActivationTime:   activationTime,
						DeactivationTime: deactivationTime,
					},
				)
			}
		}

		for _, def := range ruleInstall.RuleDefinitions {
			dynamicRulesToInstall = append(
				dynamicRulesToInstall,
				&protos.DynamicRuleInstall{
					PolicyRule:       def.ToProto(),
					ActivationTime:   activationTime,
					DeactivationTime: deactivationTime,
				},
			)
		}
	}
	return staticRulesToInstall, dynamicRulesToInstall
}

func ParseRuleRemoveAVPs(policyDBClient policydb.PolicyDBClient, rulesToRemoveAVP []*RuleRemoveAVP) []string {
	var ruleNames []string
	for _, rule := range rulesToRemoveAVP {
		ruleNames = append(ruleNames, rule.RuleNames...)
		if len(rule.RuleBaseNames) > 0 {
			ruleNames = append(ruleNames, policyDBClient.GetRuleIDsForBaseNames(rule.RuleBaseNames)...)
		}
	}
	return ruleNames
}

func GetEventTriggersRelatedInfo(
	eventTriggers []EventTrigger,
	revalidationTime *time.Time,
) ([]protos.EventTrigger, *timestamp.Timestamp) {
	protoEventTriggers := make([]protos.EventTrigger, 0, len(eventTriggers))
	var protoRevalidationTime *timestamp.Timestamp
	for _, eventTrigger := range eventTriggers {
		if convertedEventTrigger, ok := eventTriggerConversionMap[eventTrigger]; ok {
			protoEventTriggers = append(protoEventTriggers, convertedEventTrigger)
			if eventTrigger == RevalidationTimeout {
				protoRevalidationTime = ConvertToProtoTimestamp(revalidationTime)
			}
		} else {
			protoEventTriggers = append(protoEventTriggers, protos.EventTrigger_UNSUPPORTED)
		}
	}
	return protoEventTriggers, protoRevalidationTime
}

func getUsageMonitoringCredits(usageMonitors []*UsageMonitoringInfo) []*protos.UsageMonitoringCredit {
	usageMonitoringCredits := make([]*protos.UsageMonitoringCredit, 0, len(usageMonitors))
	for _, monitor := range usageMonitors {
		usageMonitoringCredits = append(
			usageMonitoringCredits,
			monitor.ToUsageMonitoringCredit(),
		)
	}
	return usageMonitoringCredits
}

func getQoSInfo(qosInfo *QosInformation) *protos.QoSInformation {
	if qosInfo == nil {
		return nil
	}
	res := &protos.QoSInformation{
		BearerId: qosInfo.BearerIdentifier,
	}
	if qosInfo.Qci != nil {
		res.Qci = protos.QCI(*qosInfo.Qci)
	}
	return res
}

func (report *UsageReport) FromUsageMonitorUpdate(update *protos.UsageMonitorUpdate) *UsageReport {
	report.MonitoringKey = update.MonitoringKey
	report.Level = MonitoringLevel(update.Level)
	report.InputOctets = update.BytesTx
	report.OutputOctets = update.BytesRx // receive == output
	report.TotalOctets = update.BytesTx + update.BytesRx
	return report
}

func (monitor *UsageMonitoringInfo) ToUsageMonitoringCredit() *protos.UsageMonitoringCredit {
	if monitor.GrantedServiceUnit == nil || monitor.GrantedServiceUnit.IsEmpty() {
		return &protos.UsageMonitoringCredit{
			Action:        protos.UsageMonitoringCredit_DISABLE,
			MonitoringKey: monitor.MonitoringKey,
			Level:         protos.MonitoringLevel(monitor.Level),
		}
	} else {
		return &protos.UsageMonitoringCredit{
			Action:        protos.UsageMonitoringCredit_CONTINUE,
			MonitoringKey: monitor.MonitoringKey,
			GrantedUnits:  monitor.GrantedServiceUnit.ToProto(),
			Level:         protos.MonitoringLevel(monitor.Level),
		}
	}
}

func GetRATType(pRATType protos.RATType) credit_control.RATType {
	switch pRATType {
	case protos.RATType_TGPP_LTE:
		return credit_control.RAT_EUTRAN
	case protos.RATType_TGPP_WLAN:
		return credit_control.RAT_WLAN
	default:
		return credit_control.RAT_EUTRAN
	}
}

// Since we don't specify the IP CAN type at session initialization, and we
// only support WLAN and EUTRAN, we will infer the IP CAN type from RAT type.
func GetIPCANType(pRATType protos.RATType) credit_control.IPCANType {
	switch pRATType {
	case protos.RATType_TGPP_LTE:
		return credit_control.IPCAN_3GPP
	case protos.RATType_TGPP_WLAN:
		return credit_control.IPCAN_Non3GPP
	default:
		return credit_control.IPCAN_Non3GPP
	}
}
