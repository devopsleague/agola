// Copyright 2019 Sorint.lab
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied
// See the License for the specific language governing permissions and
// limitations under the License.

package action

import (
	"context"

	"github.com/sorintlab/errors"

	"agola.io/agola/internal/services/common"
	serrors "agola.io/agola/internal/services/errors"
	"agola.io/agola/internal/util"
	csapitypes "agola.io/agola/services/configstore/api/types"
	cstypes "agola.io/agola/services/configstore/types"
)

type GetSecretsRequest struct {
	ParentType cstypes.ObjectKind
	ParentRef  string

	Tree             bool
	RemoveOverridden bool
}

func (h *ActionHandler) GetSecrets(ctx context.Context, req *GetSecretsRequest) ([]*csapitypes.Secret, error) {
	var cssecrets []*csapitypes.Secret
	var err error
	switch req.ParentType {
	case cstypes.ObjectKindProjectGroup:
		cssecrets, _, err = h.configstoreClient.GetProjectGroupSecrets(ctx, req.ParentRef, req.Tree)
	case cstypes.ObjectKindProject:
		cssecrets, _, err = h.configstoreClient.GetProjectSecrets(ctx, req.ParentRef, req.Tree)
	}
	if err != nil {
		return nil, APIErrorFromRemoteError(err)
	}

	if req.RemoveOverridden {
		// remove overriden secrets
		cssecrets = common.FilterOverriddenSecrets(cssecrets)
	}

	return cssecrets, nil
}

type CreateSecretRequest struct {
	Name string

	ParentType cstypes.ObjectKind
	ParentRef  string

	Type cstypes.SecretType

	// internal secret
	Data map[string]string

	// external secret
	SecretProviderID string
	Path             string
}

func (h *ActionHandler) CreateSecret(ctx context.Context, req *CreateSecretRequest) (*csapitypes.Secret, error) {
	isVariableOwner, err := h.IsAuthUserVariableOwner(ctx, req.ParentType, req.ParentRef)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to determine ownership")
	}
	if !isVariableOwner {
		return nil, util.NewAPIError(util.ErrForbidden, util.WithAPIErrorMsg("user not authorized"))
	}

	if !util.ValidateName(req.Name) {
		return nil, util.NewAPIError(util.ErrBadRequest, util.WithAPIErrorMsgf("invalid secret name %q", req.Name), serrors.InvalidSecretName())
	}

	creq := &csapitypes.CreateUpdateSecretRequest{
		Name: req.Name,
		Type: req.Type,
		Data: req.Data,
	}

	var rs *csapitypes.Secret
	switch req.ParentType {
	case cstypes.ObjectKindProjectGroup:
		h.log.Info().Msg("creating project group secret")
		rs, _, err = h.configstoreClient.CreateProjectGroupSecret(ctx, req.ParentRef, creq)
	case cstypes.ObjectKindProject:
		h.log.Info().Msg("creating project secret")
		rs, _, err = h.configstoreClient.CreateProjectSecret(ctx, req.ParentRef, creq)
	}
	if err != nil {
		return nil, APIErrorFromRemoteError(err, util.WithAPIErrorMsg("failed to create secret"))
	}
	h.log.Info().Msgf("secret %s created, ID: %s", rs.Name, rs.ID)

	return rs, nil
}

type UpdateSecretRequest struct {
	SecretName string

	Name string

	ParentType cstypes.ObjectKind
	ParentRef  string

	Type cstypes.SecretType

	// internal secret
	Data map[string]string

	// external secret
	SecretProviderID string
	Path             string
}

func (h *ActionHandler) UpdateSecret(ctx context.Context, req *UpdateSecretRequest) (*csapitypes.Secret, error) {
	isVariableOwner, err := h.IsAuthUserVariableOwner(ctx, req.ParentType, req.ParentRef)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to determine ownership")
	}
	if !isVariableOwner {
		return nil, util.NewAPIError(util.ErrForbidden, util.WithAPIErrorMsg("user not authorized"))
	}

	if !util.ValidateName(req.Name) {
		return nil, util.NewAPIError(util.ErrBadRequest, util.WithAPIErrorMsgf("invalid secret name %q", req.Name), serrors.InvalidSecretName())
	}

	creq := &csapitypes.CreateUpdateSecretRequest{
		Name: req.Name,
		Type: req.Type,
		Data: req.Data,
	}

	var rs *csapitypes.Secret
	switch req.ParentType {
	case cstypes.ObjectKindProjectGroup:
		h.log.Info().Msg("updating project group secret")
		rs, _, err = h.configstoreClient.UpdateProjectGroupSecret(ctx, req.ParentRef, req.SecretName, creq)
	case cstypes.ObjectKindProject:
		h.log.Info().Msg("updating project secret")
		rs, _, err = h.configstoreClient.UpdateProjectSecret(ctx, req.ParentRef, req.SecretName, creq)
	}
	if err != nil {
		return nil, APIErrorFromRemoteError(err, util.WithAPIErrorMsg("failed to update secret"))
	}
	h.log.Info().Msgf("secret %s updated, ID: %s", rs.Name, rs.ID)

	return rs, nil
}

func (h *ActionHandler) DeleteSecret(ctx context.Context, parentType cstypes.ObjectKind, parentRef, name string) error {
	isVariableOwner, err := h.IsAuthUserVariableOwner(ctx, parentType, parentRef)
	if err != nil {
		return errors.Wrapf(err, "failed to determine ownership")
	}
	if !isVariableOwner {
		return util.NewAPIError(util.ErrForbidden, util.WithAPIErrorMsg("user not authorized"))
	}

	switch parentType {
	case cstypes.ObjectKindProjectGroup:
		h.log.Info().Msg("deleting project group secret")
		_, err = h.configstoreClient.DeleteProjectGroupSecret(ctx, parentRef, name)
	case cstypes.ObjectKindProject:
		h.log.Info().Msg("deleting project secret")
		_, err = h.configstoreClient.DeleteProjectSecret(ctx, parentRef, name)
	}
	if err != nil {
		return APIErrorFromRemoteError(err, util.WithAPIErrorMsg("failed to delete secret"))
	}
	return nil
}
