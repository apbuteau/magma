// Copyright (c) 2004-present Facebook All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package resolver

import (
	"context"
	"time"

	"github.com/facebookincubator/symphony/graph/ent"
	"github.com/facebookincubator/symphony/graph/ent/equipment"
	"github.com/facebookincubator/symphony/graph/ent/file"
	"github.com/facebookincubator/symphony/graph/ent/link"
	"github.com/facebookincubator/symphony/graph/ent/property"
	"github.com/facebookincubator/symphony/graph/ent/propertytype"
	"github.com/facebookincubator/symphony/graph/ent/workorder"
	"github.com/facebookincubator/symphony/graph/ent/workordertype"
	"github.com/facebookincubator/symphony/graph/graphql/models"
	"github.com/facebookincubator/symphony/graph/resolverutil"

	"github.com/AlekSi/pointer"
	"github.com/pkg/errors"
	"github.com/vektah/gqlparser/gqlerror"
	"go.uber.org/zap"
	"golang.org/x/xerrors"
)

type workOrderDefinitionResolver struct{}

func (workOrderTypeResolver) CheckListDefinitions(ctx context.Context, obj *ent.WorkOrderType) ([]*ent.CheckListItemDefinition, error) {
	return obj.QueryCheckListDefinitions().All(ctx)
}

func (workOrderDefinitionResolver) Type(ctx context.Context, obj *ent.WorkOrderDefinition) (*ent.WorkOrderType, error) {
	return obj.QueryType().Only(ctx)
}

type workOrderTypeResolver struct{}

func (workOrderTypeResolver) PropertyTypes(ctx context.Context, obj *ent.WorkOrderType) ([]*ent.PropertyType, error) {
	return obj.QueryPropertyTypes().All(ctx)
}

func (workOrderTypeResolver) NumberOfWorkOrders(ctx context.Context, obj *ent.WorkOrderType) (int, error) {
	return obj.QueryWorkOrders().Count(ctx)
}

type workOrderResolver struct{}

func (workOrderResolver) WorkOrderType(ctx context.Context, obj *ent.WorkOrder) (*ent.WorkOrderType, error) {
	return obj.QueryType().Only(ctx)
}

func (workOrderResolver) Location(ctx context.Context, obj *ent.WorkOrder) (*ent.Location, error) {
	l, err := obj.QueryLocation().Only(ctx)
	return l, ent.MaskNotFound(err)
}

func (workOrderResolver) Project(ctx context.Context, obj *ent.WorkOrder) (*ent.Project, error) {
	p, err := obj.QueryProject().Only(ctx)
	return p, ent.MaskNotFound(err)
}

func (workOrderResolver) CreationDate(ctx context.Context, obj *ent.WorkOrder) (int, error) {
	secs := int(obj.CreationDate.Unix())
	return secs, nil
}

func (workOrderResolver) InstallDate(ctx context.Context, obj *ent.WorkOrder) (*int, error) {
	secs := int(obj.InstallDate.Unix())
	return &secs, nil
}

func (workOrderResolver) Status(ctx context.Context, obj *ent.WorkOrder) (models.WorkOrderStatus, error) {
	return models.WorkOrderStatus(obj.Status), nil
}

func (workOrderResolver) EquipmentToAdd(ctx context.Context, obj *ent.WorkOrder) ([]*ent.Equipment, error) {
	return obj.QueryEquipment().Where(equipment.FutureState(models.FutureStateInstall.String())).All(ctx)
}

func (workOrderResolver) EquipmentToRemove(ctx context.Context, obj *ent.WorkOrder) ([]*ent.Equipment, error) {
	return obj.QueryEquipment().Where(equipment.FutureState(models.FutureStateRemove.String())).All(ctx)
}

func (workOrderResolver) LinksToAdd(ctx context.Context, obj *ent.WorkOrder) ([]*ent.Link, error) {
	return obj.QueryLinks().Where(link.FutureState(models.FutureStateInstall.String())).All(ctx)
}

func (workOrderResolver) LinksToRemove(ctx context.Context, obj *ent.WorkOrder) ([]*ent.Link, error) {
	return obj.QueryLinks().Where(link.FutureState(models.FutureStateRemove.String())).All(ctx)
}

func (workOrderResolver) Properties(ctx context.Context, obj *ent.WorkOrder) ([]*ent.Property, error) {
	return obj.QueryProperties().All(ctx)
}

func (workOrderResolver) CheckList(ctx context.Context, obj *ent.WorkOrder) ([]*ent.CheckListItem, error) {
	return obj.QueryCheckListItems().All(ctx)
}

func (workOrderResolver) Priority(ctx context.Context, obj *ent.WorkOrder) (models.WorkOrderPriority, error) {
	return models.WorkOrderPriority(obj.Priority), nil
}

func (workOrderResolver) Images(ctx context.Context, obj *ent.WorkOrder) ([]*ent.File, error) {
	return obj.QueryFiles().Where(file.Type(models.FileTypeImage.String())).All(ctx)
}

func (workOrderResolver) Files(ctx context.Context, obj *ent.WorkOrder) ([]*ent.File, error) {
	return obj.QueryFiles().Where(file.Type(models.FileTypeFile.String())).All(ctx)
}

func (workOrderResolver) Hyperlinks(ctx context.Context, obj *ent.WorkOrder) ([]*ent.Hyperlink, error) {
	return obj.QueryHyperlinks().All(ctx)
}

func (workOrderResolver) Comments(ctx context.Context, obj *ent.WorkOrder) ([]*ent.Comment, error) {
	return obj.QueryComments().All(ctx)
}

func (r mutationResolver) AddWorkOrder(
	ctx context.Context, input models.AddWorkOrderInput,
) (*ent.WorkOrder, error) {
	c := r.ClientFrom(ctx)
	mutation := r.ClientFrom(ctx).
		WorkOrder.Create().
		SetName(input.Name).
		SetTypeID(input.WorkOrderTypeID).
		SetNillableProjectID(input.ProjectID).
		SetNillableLocationID(input.LocationID).
		SetNillableDescription(input.Description).
		SetOwnerName(r.User(ctx).email).
		SetCreationDate(time.Now()).
		SetNillableAssignee(input.Assignee).
		SetNillableIndex(input.Index)
	if input.Status != nil {
		mutation.SetStatus(input.Status.String())
	}
	if input.Priority != nil {
		mutation.SetPriority(input.Priority.String())
	}
	wo, err := mutation.Save(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "creating work order")
	}
	if _, err := r.AddProperties(input.Properties,
		resolverutil.AddPropertyArgs{
			Context:    ctx,
			EntSetter:  func(b *ent.PropertyCreate) { b.SetWorkOrderID(wo.ID) },
			IsTemplate: pointer.ToBool(true),
		},
	); err != nil {
		return nil, errors.Wrap(err, "creating work order properties")
	}
	for _, clInput := range input.CheckList {
		if _, err = c.CheckListItem.Create().
			SetTitle(clInput.Title).
			SetType(clInput.Type.String()).
			SetNillableIndex(clInput.Index).
			SetNillableEnumValues(clInput.EnumValues).
			SetNillableHelpText(clInput.HelpText).
			SetNillableChecked(clInput.Checked).
			SetNillableStringVal(clInput.StringValue).
			SetWorkOrderID(wo.ID).
			Save(ctx); err != nil {
			return nil, errors.Wrap(err, "creating check list item")
		}
	}
	return wo, nil
}

func (r mutationResolver) EditWorkOrder(
	ctx context.Context, input models.EditWorkOrderInput,
) (*ent.WorkOrder, error) {
	client := r.ClientFrom(ctx)
	wo, err := client.WorkOrder.Get(ctx, input.ID)
	if err != nil {
		return nil, errors.Wrap(err, "querying work order")
	}
	mutation := client.WorkOrder.
		UpdateOne(wo).
		SetName(input.Name).
		SetNillableDescription(input.Description).
		SetNillableAssignee(input.Assignee).
		SetOwnerName(input.OwnerName).
		SetStatus(input.Status.String()).
		SetPriority(input.Priority.String()).
		SetNillableIndex(input.Index)
	if input.InstallDate != nil {
		mutation.SetInstallDate(*input.InstallDate)
	} else {
		mutation.ClearInstallDate()
	}
	if input.ProjectID != nil {
		mutation.SetProjectID(*input.ProjectID)
	} else {
		mutation.ClearProject()
	}
	if input.LocationID != nil {
		mutation.SetLocationID(*input.LocationID)
	} else {
		mutation.ClearLocation()
	}

	wotID, err := wo.QueryType().OnlyID(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "querying work order type id")
	}
	var added, edited []*models.PropertyInput
	for _, input := range input.Properties {
		if input.ID == nil {
			added = append(added, input)
		} else {
			edited = append(edited, input)
		}
	}
	if _, err := r.AddProperties(added,
		resolverutil.AddPropertyArgs{
			Context:    ctx,
			EntSetter:  func(b *ent.PropertyCreate) { b.SetWorkOrderID(input.ID) },
			IsTemplate: pointer.ToBool(true),
		},
	); err != nil {
		return nil, err
	}
	for _, input := range edited {
		typ, err := client.WorkOrderType.Query().
			Where(workordertype.ID(wotID)).
			QueryPropertyTypes().
			Where(propertytype.ID(input.PropertyTypeID)).
			Only(ctx)
		if err != nil {
			return nil, errors.Wrapf(err, "querying work order property type %q", input.PropertyTypeID)
		}
		if typ.Editable && typ.IsInstanceProperty {
			query := client.Property.
				Update().
				Where(
					property.HasWorkOrderWith(workorder.ID(wo.ID)),
					property.ID(*input.ID),
				)
			if _, err := updatePropValues(input, query).Save(ctx); err != nil {
				return nil, errors.Wrap(err, "updating property values")
			}
		}
	}
	ids := make([]string, 0, len(input.CheckList))
	for _, clInput := range input.CheckList {
		cli, err := r.createOrUpdateCheckListItem(ctx, clInput)
		if err != nil {
			return nil, err
		}
		ids = append(ids, cli.ID)
	}
	currentCL := wo.QueryCheckListItems().IDsX(ctx)
	addedCLIds, deletedCLIds := resolverutil.GetDifferenceBetweenSlices(currentCL, ids)
	mutation.
		RemoveCheckListItemIDs(deletedCLIds...).
		AddCheckListItemIDs(addedCLIds...)
	return mutation.Save(ctx)
}

func (r mutationResolver) createOrUpdateCheckListItem(
	ctx context.Context,
	clInput *models.CheckListItemInput) (*ent.CheckListItem, error) {
	client := r.ClientFrom(ctx)
	cl := client.CheckListItem
	if clInput.ID == nil {
		cli, err := cl.Create().
			SetTitle(clInput.Title).
			SetType(clInput.Type.String()).
			SetNillableIndex(clInput.Index).
			SetNillableEnumValues(clInput.EnumValues).
			SetNillableHelpText(clInput.HelpText).
			SetNillableChecked(clInput.Checked).
			SetNillableStringVal(clInput.StringValue).
			Save(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "creating check list item")
		}
		return cli, nil
	} else {
		cli, err := cl.UpdateOneID(*clInput.ID).
			SetTitle(clInput.Title).
			SetType(clInput.Type.String()).
			SetNillableIndex(clInput.Index).
			SetNillableEnumValues(clInput.EnumValues).
			SetNillableHelpText(clInput.HelpText).
			SetNillableChecked(clInput.Checked).
			SetNillableStringVal(clInput.StringValue).
			Save(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "updating check list item")
		}
		return cli, nil
	}
}

func (r mutationResolver) AddWorkOrderType(
	ctx context.Context, input models.AddWorkOrderTypeInput) (*ent.WorkOrderType, error) {
	props, err := r.AddPropertyTypes(ctx, input.Properties...)
	if err != nil {
		return nil, err
	}

	client := r.ClientFrom(ctx)
	typ, err := client.WorkOrderType.
		Create().
		SetName(input.Name).
		SetNillableDescription(input.Description).
		AddPropertyTypes(props...).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, gqlerror.Errorf("A work order type with the name %v already exists", input.Name)
		}
		return nil, errors.Wrap(err, "creating work order type")
	}

	for _, def := range input.CheckList {
		def := def
		if _, err = client.CheckListItemDefinition.Create().
			SetTitle(def.Title).
			SetType(def.Type.String()).
			SetNillableIndex(def.Index).
			SetNillableHelpText(def.HelpText).
			SetNillableEnumValues(def.EnumValues).
			SetWorkOrderType(typ).
			Save(ctx); err != nil {
			return nil, errors.Wrap(err, "creating check list item")
		}
	}
	return typ, nil
}

func (r mutationResolver) EditWorkOrderType(
	ctx context.Context, input models.EditWorkOrderTypeInput,
) (*ent.WorkOrderType, error) {
	client := r.ClientFrom(ctx)
	wot, err := client.WorkOrderType.Get(ctx, input.ID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, gqlerror.Errorf("A work order template with id=%q does not exist", input.ID)
		}
		if ent.IsConstraintError(err) {
			return nil, gqlerror.Errorf("A work order template with the name %v already exists", input.Name)
		}
		return nil, errors.Wrapf(err, "updating work order template: id=%q", input.ID)
	}
	for _, p := range input.Properties {
		if p.ID == nil {
			err = r.validateAndAddNewPropertyType(ctx, p, func(b *ent.PropertyTypeUpdateOne) { b.SetWorkOrderTypeID(input.ID) })
		} else {
			err = r.updatePropType(ctx, p)
		}
		if err != nil {
			return nil, err
		}
	}

	mutation := client.WorkOrderType.
		UpdateOneID(input.ID).
		SetName(input.Name).
		SetNillableDescription(input.Description)

	currentCL := wot.QueryCheckListDefinitions().IDsX(ctx)
	ids := make([]string, 0, len(input.CheckList))
	for _, clInput := range input.CheckList {
		cli, err := r.createOrUpdateCheckListDefinition(ctx, clInput, input.ID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, cli.ID)
	}
	_, deletedCLIds := resolverutil.GetDifferenceBetweenSlices(currentCL, ids)
	mutation.RemoveCheckListDefinitionIDs(deletedCLIds...)
	return mutation.Save(ctx)
}

func (r mutationResolver) createOrUpdateCheckListDefinition(
	ctx context.Context,
	clInput *models.CheckListDefinitionInput,
	wotID string) (*ent.CheckListItemDefinition, error) {

	client := r.ClientFrom(ctx)
	cl := client.CheckListItemDefinition
	if clInput.ID == nil {
		cli, err := cl.Create().
			SetTitle(clInput.Title).
			SetType(clInput.Type.String()).
			SetNillableIndex(clInput.Index).
			SetNillableEnumValues(clInput.EnumValues).
			SetNillableHelpText(clInput.HelpText).
			SetWorkOrderTypeID(wotID).
			Save(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "creating check list definition")
		}
		return cli, nil
	}

	cli, err := cl.UpdateOneID(*clInput.ID).
		SetTitle(clInput.Title).
		SetType(clInput.Type.String()).
		SetNillableIndex(clInput.Index).
		SetNillableEnumValues(clInput.EnumValues).
		SetNillableHelpText(clInput.HelpText).
		Save(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "updating check list definition")
	}
	return cli, nil
}

func (r mutationResolver) RemoveWorkOrderType(ctx context.Context, id string) (string, error) {
	client, logger := r.ClientFrom(ctx), r.log.For(ctx).With(zap.String("id", id))
	switch count, err := client.WorkOrderType.Query().
		Where(workordertype.ID(id)).
		QueryWorkOrders().
		Count(ctx); {
	case err != nil:
		logger.Error("cannot query work order count of type", zap.Error(err))
		return "", xerrors.Errorf("querying work orders for type: %w", err)
	case count > 0:
		logger.Warn("work order type has existing work orders", zap.Int("count", count))
		return "", gqlerror.Errorf("cannot delete work order type with %d existing work orders", count)
	}
	if _, err := client.PropertyType.Delete().
		Where(propertytype.HasWorkOrderTypeWith(workordertype.ID(id))).
		Exec(ctx); err != nil {
		logger.Error("cannot delete properties of work order type", zap.Error(err))
		return "", xerrors.Errorf("deleting work order property types: %w", err)
	}
	switch err := client.WorkOrderType.DeleteOneID(id).Exec(ctx); err.(type) {
	case nil:
		logger.Info("deleted work order type")
		return id, nil
	case *ent.ErrNotFound:
		err := gqlerror.Errorf("work order type not found")
		logger.Error(err.Message)
		return "", err
	default:
		logger.Error("cannot delete work order type", zap.Error(err))
		return "", xerrors.Errorf("deleting work order type: %w", err)
	}
}
