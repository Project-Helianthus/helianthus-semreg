package call

import (
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func project(completion driver.Completion, relation operation.ReadbackRelation, qualified bool) driver.OperationResult {
	result := driver.OperationResult{CompoundExecution: completion.CompoundExecution}
	if completion.Acknowledgement != nil {
		result.Acknowledgement = completion.Acknowledgement.State
	}
	if completion.Dispatch != nil {
		result.Delivery = completion.Dispatch.Delivery
	}
	if completion.Readback != nil {
		result.ReadbackRelation = relation
	}
	outcome := derive(completion, relation, qualified)
	result.KernelOutcome = outcome
	if outcome == operation.OutcomeFailedNoContact {
		result.Reason = ReasonNoContact
	}
	switch outcome {
	case operation.OutcomeApplied:
		result.Status = StatusReadbackConfirmed
	case operation.OutcomeAcknowledgedUnverified:
		result.Status = StatusProtocolAck
	case operation.OutcomeIndeterminate:
		result.Status = StatusUnknown
	case "":
		result.Status = StatusAccepted
	default:
		result.Status = StatusFailed
	}
	return applyCompound(result, completion.CompoundExecution)
}

func derive(completion driver.Completion, relation operation.ReadbackRelation, qualified bool) operation.Outcome {
	if completion.Outcome == "" && completion.Dispatch == nil && completion.Acknowledgement == nil && completion.Readback == nil {
		return ""
	}
	dispatch := completion.Dispatch
	if dispatch == nil || dispatch.Delivery == operation.DeliveryNotSent && !dispatch.PossibleSideEffect {
		return operation.OutcomeFailedNoContact
	}
	sent := dispatch.Delivery == operation.DeliverySent
	possible := dispatch.PossibleSideEffect || dispatch.Delivery == operation.DeliveryUnknown || dispatch.Delivery == operation.DeliverySent
	if sent && possible && qualified && relation == operation.ReadbackConfirms && dispatch.Completed != nil {
		return operation.OutcomeApplied
	}
	if sent && possible && relation == operation.ReadbackContradicts && completion.Readback != nil {
		return operation.OutcomeConflict
	}
	if sent && completion.Acknowledgement != nil && completion.Acknowledgement.State == operation.AckRejected {
		return operation.OutcomeNoEffect
	}
	if sent && possible && completion.Acknowledgement != nil && (completion.Acknowledgement.State == operation.AckAccepted || completion.Acknowledgement.State == operation.AckProvisional) && (completion.Readback == nil || relation == operation.ReadbackInconclusive || !qualified) {
		return operation.OutcomeAcknowledgedUnverified
	}
	if possible {
		return operation.OutcomeIndeterminate
	}
	return operation.OutcomeFailedNoContact
}

func applyCompound(result driver.OperationResult, compound *driver.CompoundExecution) driver.OperationResult {
	if compound == nil {
		return result
	}
	singleAck := len(compound.Steps) == 1 && compound.Steps[0].Attempted && (compound.Steps[0].Acknowledgement == operation.AckAccepted || compound.Steps[0].Acknowledgement == operation.AckProvisional)
	partial := compound.Partial || unattempted(compound.Steps) || singleAck
	if partial && result.Status == StatusReadbackConfirmed {
		if result.Acknowledgement == operation.AckAccepted || result.Acknowledgement == operation.AckProvisional {
			result.Status = StatusProtocolAck
			result.KernelOutcome = operation.OutcomeAcknowledgedUnverified
			if result.ReadbackRelation == operation.ReadbackConfirms {
				result.ReadbackRelation = operation.ReadbackInconclusive
			}
		} else {
			result.Status = StatusUnknown
			result.KernelOutcome = operation.OutcomeIndeterminate
		}
	}
	if result.KernelOutcome == operation.OutcomeFailedNoContact && sideEffect(compound.Steps) {
		result.Status = StatusUnknown
		result.KernelOutcome = operation.OutcomeIndeterminate
		result.Delivery = operation.DeliveryUnknown
	}
	return result
}

func unattempted(steps []driver.CompoundStep) bool {
	for _, step := range steps {
		if !step.Attempted {
			return true
		}
	}
	return false
}

func sideEffect(steps []driver.CompoundStep) bool {
	for _, step := range steps {
		if step.Delivery == operation.DeliverySent || step.Delivery == operation.DeliveryUnknown {
			return true
		}
	}
	return false
}
