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

package api

import (
	"net/http"

	"github.com/rs/zerolog"

	"agola.io/agola/internal/services/common"
	"agola.io/agola/internal/services/gateway/action"
	"agola.io/agola/internal/services/types"
	"agola.io/agola/internal/util"
	csclient "agola.io/agola/services/configstore/client"
	cstypes "agola.io/agola/services/configstore/types"
	rsclient "agola.io/agola/services/runservice/client"
)

type webhooksHandler struct {
	log               zerolog.Logger
	ah                *action.ActionHandler
	configstoreClient *csclient.Client
	runserviceClient  *rsclient.Client
	apiExposedURL     string
}

func NewWebhooksHandler(log zerolog.Logger, ah *action.ActionHandler, configstoreClient *csclient.Client, runserviceClient *rsclient.Client, apiExposedURL string) *webhooksHandler {
	return &webhooksHandler{
		log:               log,
		ah:                ah,
		configstoreClient: configstoreClient,
		runserviceClient:  runserviceClient,
		apiExposedURL:     apiExposedURL,
	}
}

func (h *webhooksHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := h.do(r)
	if util.HTTPError(w, err) {
		h.log.Err(err).Send()
		return
	}
}

func (h *webhooksHandler) do(r *http.Request) error {
	ctx := r.Context()

	projectID := r.URL.Query().Get("projectid")
	if projectID == "" {
		return util.NewAPIError(util.ErrBadRequest, util.WithAPIErrorMsgf("bad webhook url %q. Missing projectid", r.URL))
	}

	defer r.Body.Close()

	csProject, _, err := h.configstoreClient.GetProject(ctx, projectID)
	if err != nil {
		return action.APIErrorFromRemoteError(err, util.WithAPIErrorMsgf("failed to get project %s", projectID))
	}
	project := csProject.Project

	user, _, err := h.configstoreClient.GetUserByLinkedAccount(ctx, project.LinkedAccountID)
	if err != nil {
		return action.APIErrorFromRemoteError(err, util.WithAPIErrorMsgf("failed to get user by linked account %q", project.LinkedAccountID))
	}
	linkedAccounts, _, err := h.configstoreClient.GetUserLinkedAccounts(ctx, user.ID)
	if err != nil {
		return action.APIErrorFromRemoteError(err, util.WithAPIErrorMsgf("failed to get user %q linked accounts", user.ID))
	}

	var la *cstypes.LinkedAccount
	for _, v := range linkedAccounts {
		if v.ID == project.LinkedAccountID {
			la = v
			break
		}
	}

	if la == nil {
		return util.NewAPIError(util.ErrInternal, util.WithAPIErrorMsgf("linked account %q for user %q doesn't exist", project.LinkedAccountID, user.Name))
	}

	rs, _, err := h.configstoreClient.GetRemoteSource(ctx, la.RemoteSourceID)
	if err != nil {
		return action.APIErrorFromRemoteError(err, util.WithAPIErrorMsgf("failed to get remote source %q", la.RemoteSourceID))
	}

	gitSource, err := h.ah.GetGitSource(ctx, rs, user.Name, la)
	if err != nil {
		return util.NewAPIErrorWrap(util.ErrInternal, err, util.WithAPIErrorMsg("failed to create gitea client"))
	}

	sshPrivKey := project.SSHPrivateKey
	sshHostKey := rs.SSHHostKey
	// use remotesource skipSSHHostKeyCheck config and override with project config if set to true there
	skipSSHHostKeyCheck := rs.SkipSSHHostKeyCheck
	if project.SkipSSHHostKeyCheck {
		skipSSHHostKeyCheck = project.SkipSSHHostKeyCheck
	}

	webhookData, err := gitSource.ParseWebhook(r, project.WebhookSecret)
	if err != nil {
		return util.NewAPIErrorWrap(util.ErrBadRequest, err, util.WithAPIErrorMsg("failed to parse webhook"))
	}
	// skip nil webhook data
	// TODO(sgotti) report the reason of the skip
	if webhookData == nil {
		h.log.Info().Msg("skipping webhook")
		return nil
	}

	cloneURL := webhookData.SSHURL

	req := &action.CreateRunRequest{
		RunType:            types.RunTypeProject,
		RefType:            common.WebHookEventToRunRefType(webhookData.Event),
		RunCreationTrigger: types.RunCreationTriggerTypeWebhook,

		Project:             project,
		User:                nil,
		RepoPath:            webhookData.Repo.Path,
		GitSource:           gitSource,
		CommitSHA:           webhookData.CommitSHA,
		Message:             webhookData.Message,
		Branch:              webhookData.Branch,
		Tag:                 webhookData.Tag,
		PullRequestID:       webhookData.PullRequestID,
		PRFromSameRepo:      webhookData.PRFromSameRepo,
		Ref:                 webhookData.Ref,
		SSHPrivKey:          sshPrivKey,
		SSHHostKey:          sshHostKey,
		SkipSSHHostKeyCheck: skipSSHHostKeyCheck,
		CloneURL:            cloneURL,

		CommitLink:      webhookData.CommitLink,
		BranchLink:      webhookData.BranchLink,
		TagLink:         webhookData.TagLink,
		PullRequestLink: webhookData.PullRequestLink,
		CompareLink:     webhookData.CompareLink,
	}
	if err := h.ah.CreateRuns(ctx, req); err != nil {
		return util.NewAPIErrorWrap(util.ErrInternal, err, util.WithAPIErrorMsg("failed to create run"))
	}

	return nil
}
