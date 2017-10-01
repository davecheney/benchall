// Copyright 2014 The Gogs Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package repo

import (
	"container/list"
	"path"
	"strings"

	"github.com/Unknwon/com"

	"github.com/gogits/git-module"

	"github.com/gogits/gogs/models"
	"github.com/gogits/gogs/modules/auth"
	"github.com/gogits/gogs/modules/base"
	"github.com/gogits/gogs/modules/context"
	"github.com/gogits/gogs/modules/log"
	"github.com/gogits/gogs/modules/setting"
)

const (
	FORK         base.TplName = "repo/pulls/fork"
	COMPARE_PULL base.TplName = "repo/pulls/compare"
	PULL_COMMITS base.TplName = "repo/pulls/commits"
	PULL_FILES   base.TplName = "repo/pulls/files"

	PULL_REQUEST_TEMPLATE_KEY = "PullRequestTemplate"
)

var (
	PullRequestTemplateCandidates = []string{
		"PULL_REQUEST.md",
		".gogs/PULL_REQUEST.md",
		".github/PULL_REQUEST.md",
	}
)

func getForkRepository(ctx *context.Context) *models.Repository {
	forkRepo, err := models.GetRepositoryByID(ctx.ParamsInt64(":repoid"))
	if err != nil {
		if models.IsErrRepoNotExist(err) {
			ctx.Handle(404, "GetRepositoryByID", nil)
		} else {
			ctx.Handle(500, "GetRepositoryByID", err)
		}
		return nil
	}

	if !forkRepo.CanBeForked() {
		ctx.Handle(404, "getForkRepository", nil)
		return nil
	}

	ctx.Data["repo_name"] = forkRepo.Name
	ctx.Data["description"] = forkRepo.Description
	ctx.Data["IsPrivate"] = forkRepo.IsPrivate

	if err = forkRepo.GetOwner(); err != nil {
		ctx.Handle(500, "GetOwner", err)
		return nil
	}
	ctx.Data["ForkFrom"] = forkRepo.Owner.Name + "/" + forkRepo.Name

	if err := ctx.User.GetOrganizations(true); err != nil {
		ctx.Handle(500, "GetOrganizations", err)
		return nil
	}
	ctx.Data["Orgs"] = ctx.User.Orgs

	return forkRepo
}

func Fork(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("new_fork")

	getForkRepository(ctx)
	if ctx.Written() {
		return
	}

	ctx.Data["ContextUser"] = ctx.User
	ctx.HTML(200, FORK)
}

func ForkPost(ctx *context.Context, form auth.CreateRepoForm) {
	ctx.Data["Title"] = ctx.Tr("new_fork")

	forkRepo := getForkRepository(ctx)
	if ctx.Written() {
		return
	}

	ctxUser := checkContextUser(ctx, form.Uid)
	if ctx.Written() {
		return
	}
	ctx.Data["ContextUser"] = ctxUser

	if ctx.HasError() {
		ctx.HTML(200, FORK)
		return
	}

	repo, has := models.HasForkedRepo(ctxUser.Id, forkRepo.ID)
	if has {
		ctx.Redirect(setting.AppSubUrl + "/" + ctxUser.Name + "/" + repo.Name)
		return
	}

	// Check ownership of organization.
	if ctxUser.IsOrganization() {
		if !ctxUser.IsOwnedBy(ctx.User.Id) {
			ctx.Error(403)
			return
		}
	}

	repo, err := models.ForkRepository(ctxUser, forkRepo, form.RepoName, form.Description)
	if err != nil {
		ctx.Data["Err_RepoName"] = true
		switch {
		case models.IsErrRepoAlreadyExist(err):
			ctx.RenderWithErr(ctx.Tr("repo.settings.new_owner_has_same_repo"), FORK, &form)
		case models.IsErrNameReserved(err):
			ctx.RenderWithErr(ctx.Tr("repo.form.name_reserved", err.(models.ErrNameReserved).Name), FORK, &form)
		case models.IsErrNamePatternNotAllowed(err):
			ctx.RenderWithErr(ctx.Tr("repo.form.name_pattern_not_allowed", err.(models.ErrNamePatternNotAllowed).Pattern), FORK, &form)
		default:
			ctx.Handle(500, "ForkPost", err)
		}
		return
	}

	log.Trace("Repository forked[%d]: %s/%s", forkRepo.ID, ctxUser.Name, repo.Name)
	ctx.Redirect(setting.AppSubUrl + "/" + ctxUser.Name + "/" + repo.Name)
}

func checkPullInfo(ctx *context.Context) *models.Issue {
	issue, err := models.GetIssueByIndex(ctx.Repo.Repository.ID, ctx.ParamsInt64(":index"))
	if err != nil {
		if models.IsErrIssueNotExist(err) {
			ctx.Handle(404, "GetIssueByIndex", err)
		} else {
			ctx.Handle(500, "GetIssueByIndex", err)
		}
		return nil
	}
	ctx.Data["Title"] = issue.Name
	ctx.Data["Issue"] = issue

	if !issue.IsPull {
		ctx.Handle(404, "ViewPullCommits", nil)
		return nil
	}

	if err = issue.GetPullRequest(); err != nil {
		ctx.Handle(500, "GetPullRequest", err)
		return nil
	} else if err = issue.GetHeadRepo(); err != nil {
		ctx.Handle(500, "GetHeadRepo", err)
		return nil
	}

	if ctx.IsSigned {
		// Update issue-user.
		if err = issue.ReadBy(ctx.User.Id); err != nil {
			ctx.Handle(500, "ReadBy", err)
			return nil
		}
	}

	return issue
}

func PrepareMergedViewPullInfo(ctx *context.Context, pull *models.Issue) {
	ctx.Data["HasMerged"] = true

	var err error

	if err = pull.GetMerger(); err != nil {
		ctx.Handle(500, "GetMerger", err)
		return
	}

	ctx.Data["HeadTarget"] = pull.HeadUserName + "/" + pull.HeadBranch
	ctx.Data["BaseTarget"] = ctx.Repo.Owner.Name + "/" + pull.BaseBranch

	ctx.Data["NumCommits"], err = ctx.Repo.GitRepo.CommitsCountBetween(pull.MergeBase, pull.MergedCommitID)
	if err != nil {
		ctx.Handle(500, "Repo.GitRepo.CommitsCountBetween", err)
		return
	}
	ctx.Data["NumFiles"], err = ctx.Repo.GitRepo.FilesCountBetween(pull.MergeBase, pull.MergedCommitID)
	if err != nil {
		ctx.Handle(500, "Repo.GitRepo.FilesCountBetween", err)
		return
	}
}

func PrepareViewPullInfo(ctx *context.Context, pull *models.Issue) *git.PullRequestInfo {
	repo := ctx.Repo.Repository

	ctx.Data["HeadTarget"] = pull.HeadUserName + "/" + pull.HeadBranch
	ctx.Data["BaseTarget"] = ctx.Repo.Owner.Name + "/" + pull.BaseBranch

	var (
		headGitRepo *git.Repository
		err         error
	)

	if err = pull.GetHeadRepo(); err != nil {
		ctx.Handle(500, "GetHeadRepo", err)
		return nil
	}

	if pull.HeadRepo != nil {
		headGitRepo, err = git.OpenRepository(pull.HeadRepo.RepoPath())
		if err != nil {
			ctx.Handle(500, "OpenRepository", err)
			return nil
		}
	}

	if pull.HeadRepo == nil || !headGitRepo.IsBranchExist(pull.HeadBranch) {
		ctx.Data["IsPullReuqestBroken"] = true
		ctx.Data["HeadTarget"] = "deleted"
		ctx.Data["NumCommits"] = 0
		ctx.Data["NumFiles"] = 0
		return nil
	}

	prInfo, err := headGitRepo.GetPullRequestInfo(models.RepoPath(repo.Owner.Name, repo.Name),
		pull.BaseBranch, pull.HeadBranch)
	if err != nil {
		ctx.Handle(500, "GetPullRequestInfo", err)
		return nil
	}
	ctx.Data["NumCommits"] = prInfo.Commits.Len()
	ctx.Data["NumFiles"] = prInfo.NumFiles
	return prInfo
}

func ViewPullCommits(ctx *context.Context) {
	ctx.Data["PageIsPullCommits"] = true

	pull := checkPullInfo(ctx)
	if ctx.Written() {
		return
	}
	ctx.Data["Username"] = pull.HeadUserName
	ctx.Data["Reponame"] = pull.HeadRepo.Name

	var commits *list.List
	if pull.HasMerged {
		PrepareMergedViewPullInfo(ctx, pull)
		if ctx.Written() {
			return
		}
		startCommit, err := ctx.Repo.GitRepo.GetCommit(pull.MergeBase)
		if err != nil {
			ctx.Handle(500, "Repo.GitRepo.GetCommit", err)
			return
		}
		endCommit, err := ctx.Repo.GitRepo.GetCommit(pull.MergedCommitID)
		if err != nil {
			ctx.Handle(500, "Repo.GitRepo.GetCommit", err)
			return
		}
		commits, err = ctx.Repo.GitRepo.CommitsBetween(endCommit, startCommit)
		if err != nil {
			ctx.Handle(500, "Repo.GitRepo.CommitsBetween", err)
			return
		}

	} else {
		prInfo := PrepareViewPullInfo(ctx, pull)
		if ctx.Written() {
			return
		} else if prInfo == nil {
			ctx.Handle(404, "ViewPullCommits", nil)
			return
		}
		commits = prInfo.Commits
	}

	commits = models.ValidateCommitsWithEmails(commits)
	ctx.Data["Commits"] = commits
	ctx.Data["CommitCount"] = commits.Len()

	ctx.HTML(200, PULL_COMMITS)
}

func ViewPullFiles(ctx *context.Context) {
	ctx.Data["PageIsPullFiles"] = true

	pull := checkPullInfo(ctx)
	if ctx.Written() {
		return
	}

	var (
		diffRepoPath  string
		startCommitID string
		endCommitID   string
		gitRepo       *git.Repository
	)

	if pull.HasMerged {
		PrepareMergedViewPullInfo(ctx, pull)
		if ctx.Written() {
			return
		}

		diffRepoPath = ctx.Repo.GitRepo.Path
		startCommitID = pull.MergeBase
		endCommitID = pull.MergedCommitID
		gitRepo = ctx.Repo.GitRepo
	} else {
		prInfo := PrepareViewPullInfo(ctx, pull)
		if ctx.Written() {
			return
		} else if prInfo == nil {
			ctx.Handle(404, "ViewPullFiles", nil)
			return
		}

		headRepoPath := models.RepoPath(pull.HeadUserName, pull.HeadRepo.Name)

		headGitRepo, err := git.OpenRepository(headRepoPath)
		if err != nil {
			ctx.Handle(500, "OpenRepository", err)
			return
		}

		headCommitID, err := headGitRepo.GetBranchCommitID(pull.HeadBranch)
		if err != nil {
			ctx.Handle(500, "GetBranchCommitID", err)
			return
		}

		diffRepoPath = headRepoPath
		startCommitID = prInfo.MergeBase
		endCommitID = headCommitID
		gitRepo = headGitRepo
	}

	diff, err := models.GetDiffRange(diffRepoPath,
		startCommitID, endCommitID, setting.Git.MaxGitDiffLines)
	if err != nil {
		ctx.Handle(500, "GetDiffRange", err)
		return
	}
	ctx.Data["Diff"] = diff
	ctx.Data["DiffNotAvailable"] = diff.NumFiles() == 0

	commit, err := gitRepo.GetCommit(endCommitID)
	if err != nil {
		ctx.Handle(500, "GetCommit", err)
		return
	}

	headTarget := path.Join(pull.HeadUserName, pull.HeadRepo.Name)
	ctx.Data["Username"] = pull.HeadUserName
	ctx.Data["Reponame"] = pull.HeadRepo.Name
	ctx.Data["IsImageFile"] = commit.IsImageFile
	ctx.Data["SourcePath"] = setting.AppSubUrl + "/" + path.Join(headTarget, "src", endCommitID)
	ctx.Data["BeforeSourcePath"] = setting.AppSubUrl + "/" + path.Join(headTarget, "src", startCommitID)
	ctx.Data["RawPath"] = setting.AppSubUrl + "/" + path.Join(headTarget, "raw", endCommitID)
	ctx.Data["RequireHighlightJS"] = true

	ctx.HTML(200, PULL_FILES)
}

func MergePullRequest(ctx *context.Context) {
	issue := checkPullInfo(ctx)
	if ctx.Written() {
		return
	}
	if issue.IsClosed {
		ctx.Handle(404, "MergePullRequest", nil)
		return
	}

	pr, err := models.GetPullRequestByIssueID(issue.ID)
	if err != nil {
		if models.IsErrPullRequestNotExist(err) {
			ctx.Handle(404, "GetPullRequestByIssueID", nil)
		} else {
			ctx.Handle(500, "GetPullRequestByIssueID", err)
		}
		return
	}

	if !pr.CanAutoMerge() || pr.HasMerged {
		ctx.Handle(404, "MergePullRequest", nil)
		return
	}

	pr.Issue = issue
	pr.Issue.Repo = ctx.Repo.Repository
	if err = pr.Merge(ctx.User, ctx.Repo.GitRepo); err != nil {
		ctx.Handle(500, "Merge", err)
		return
	}

	log.Trace("Pull request merged: %d", pr.ID)
	ctx.Redirect(ctx.Repo.RepoLink + "/pulls/" + com.ToStr(pr.Index))
}

func ParseCompareInfo(ctx *context.Context) (*models.User, *models.Repository, *git.Repository, *git.PullRequestInfo, string, string) {
	baseRepo := ctx.Repo.Repository

	// Get compared branches information
	// format: <base branch>...[<head repo>:]<head branch>
	// base<-head: master...head:feature
	// same repo: master...feature
	infos := strings.Split(ctx.Params("*"), "...")
	if len(infos) != 2 {
		log.Trace("ParseCompareInfo[%d]: not enough compared branches information %s", baseRepo.ID, infos)
		ctx.Handle(404, "CompareAndPullRequest", nil)
		return nil, nil, nil, nil, "", ""
	}

	baseBranch := infos[0]
	ctx.Data["BaseBranch"] = baseBranch

	var (
		headUser   *models.User
		headBranch string
		isSameRepo bool
		err        error
	)

	// If there is no head repository, it means pull request between same repository.
	headInfos := strings.Split(infos[1], ":")
	if len(headInfos) == 1 {
		isSameRepo = true
		headUser = ctx.Repo.Owner
		headBranch = headInfos[0]

	} else if len(headInfos) == 2 {
		headUser, err = models.GetUserByName(headInfos[0])
		if err != nil {
			if models.IsErrUserNotExist(err) {
				ctx.Handle(404, "GetUserByName", nil)
			} else {
				ctx.Handle(500, "GetUserByName", err)
			}
			return nil, nil, nil, nil, "", ""
		}
		headBranch = headInfos[1]

	} else {
		ctx.Handle(404, "CompareAndPullRequest", nil)
		return nil, nil, nil, nil, "", ""
	}
	ctx.Data["HeadUser"] = headUser
	ctx.Data["HeadBranch"] = headBranch
	ctx.Repo.PullRequest.SameRepo = isSameRepo

	// Check if base branch is valid.
	if !ctx.Repo.GitRepo.IsBranchExist(baseBranch) {
		ctx.Handle(404, "IsBranchExist", nil)
		return nil, nil, nil, nil, "", ""
	}

	// Check if current user has fork of repository or in the same repository.
	headRepo, has := models.HasForkedRepo(headUser.Id, baseRepo.ID)
	if !has && !isSameRepo {
		log.Trace("ParseCompareInfo[%d]: does not have fork or in same repository", baseRepo.ID)
		ctx.Handle(404, "ParseCompareInfo", nil)
		return nil, nil, nil, nil, "", ""
	}

	var headGitRepo *git.Repository
	if isSameRepo {
		headRepo = ctx.Repo.Repository
		headGitRepo = ctx.Repo.GitRepo
	} else {
		headGitRepo, err = git.OpenRepository(models.RepoPath(headUser.Name, headRepo.Name))
		if err != nil {
			ctx.Handle(500, "OpenRepository", err)
			return nil, nil, nil, nil, "", ""
		}
	}

	if !ctx.User.IsWriterOfRepo(headRepo) && !ctx.User.IsAdmin {
		log.Trace("ParseCompareInfo[%d]: does not have write access or site admin", baseRepo.ID)
		ctx.Handle(404, "ParseCompareInfo", nil)
		return nil, nil, nil, nil, "", ""
	}

	// Check if head branch is valid.
	if !headGitRepo.IsBranchExist(headBranch) {
		ctx.Handle(404, "IsBranchExist", nil)
		return nil, nil, nil, nil, "", ""
	}

	headBranches, err := headGitRepo.GetBranches()
	if err != nil {
		ctx.Handle(500, "GetBranches", err)
		return nil, nil, nil, nil, "", ""
	}
	ctx.Data["HeadBranches"] = headBranches

	prInfo, err := headGitRepo.GetPullRequestInfo(models.RepoPath(baseRepo.Owner.Name, baseRepo.Name), baseBranch, headBranch)
	if err != nil {
		ctx.Handle(500, "GetPullRequestInfo", err)
		return nil, nil, nil, nil, "", ""
	}
	ctx.Data["BeforeCommitID"] = prInfo.MergeBase

	return headUser, headRepo, headGitRepo, prInfo, baseBranch, headBranch
}

func PrepareCompareDiff(
	ctx *context.Context,
	headUser *models.User,
	headRepo *models.Repository,
	headGitRepo *git.Repository,
	prInfo *git.PullRequestInfo,
	baseBranch, headBranch string) bool {

	var (
		repo = ctx.Repo.Repository
		err  error
	)

	// Get diff information.
	ctx.Data["CommitRepoLink"] = headRepo.RepoLink()

	headCommitID, err := headGitRepo.GetBranchCommitID(headBranch)
	if err != nil {
		ctx.Handle(500, "GetBranchCommitID", err)
		return false
	}
	ctx.Data["AfterCommitID"] = headCommitID

	if headCommitID == prInfo.MergeBase {
		ctx.Data["IsNothingToCompare"] = true
		return true
	}

	diff, err := models.GetDiffRange(models.RepoPath(headUser.Name, headRepo.Name),
		prInfo.MergeBase, headCommitID, setting.Git.MaxGitDiffLines)
	if err != nil {
		ctx.Handle(500, "GetDiffRange", err)
		return false
	}
	ctx.Data["Diff"] = diff
	ctx.Data["DiffNotAvailable"] = diff.NumFiles() == 0

	headCommit, err := headGitRepo.GetCommit(headCommitID)
	if err != nil {
		ctx.Handle(500, "GetCommit", err)
		return false
	}

	prInfo.Commits = models.ValidateCommitsWithEmails(prInfo.Commits)
	ctx.Data["Commits"] = prInfo.Commits
	ctx.Data["CommitCount"] = prInfo.Commits.Len()
	ctx.Data["Username"] = headUser.Name
	ctx.Data["Reponame"] = headRepo.Name
	ctx.Data["IsImageFile"] = headCommit.IsImageFile

	headTarget := path.Join(headUser.Name, repo.Name)
	ctx.Data["SourcePath"] = setting.AppSubUrl + "/" + path.Join(headTarget, "src", headCommitID)
	ctx.Data["BeforeSourcePath"] = setting.AppSubUrl + "/" + path.Join(headTarget, "src", prInfo.MergeBase)
	ctx.Data["RawPath"] = setting.AppSubUrl + "/" + path.Join(headTarget, "raw", headCommitID)
	return false
}

func CompareAndPullRequest(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.pulls.compare_changes")
	ctx.Data["PageIsComparePull"] = true
	ctx.Data["IsDiffCompare"] = true
	ctx.Data["RequireHighlightJS"] = true
	setTemplateIfExists(ctx, PULL_REQUEST_TEMPLATE_KEY, PullRequestTemplateCandidates)
	renderAttachmentSettings(ctx)

	headUser, headRepo, headGitRepo, prInfo, baseBranch, headBranch := ParseCompareInfo(ctx)
	if ctx.Written() {
		return
	}

	pr, err := models.GetUnmergedPullRequest(headRepo.ID, ctx.Repo.Repository.ID, headBranch, baseBranch)
	if err != nil {
		if !models.IsErrPullRequestNotExist(err) {
			ctx.Handle(500, "GetUnmergedPullRequest", err)
			return
		}
	} else {
		ctx.Data["HasPullRequest"] = true
		ctx.Data["PullRequest"] = pr
		ctx.HTML(200, COMPARE_PULL)
		return
	}

	nothingToCompare := PrepareCompareDiff(ctx, headUser, headRepo, headGitRepo, prInfo, baseBranch, headBranch)
	if ctx.Written() {
		return
	}

	if !nothingToCompare {
		// Setup information for new form.
		RetrieveRepoMetas(ctx, ctx.Repo.Repository)
		if ctx.Written() {
			return
		}
	}

	ctx.HTML(200, COMPARE_PULL)
}

func CompareAndPullRequestPost(ctx *context.Context, form auth.CreateIssueForm) {
	ctx.Data["Title"] = ctx.Tr("repo.pulls.compare_changes")
	ctx.Data["PageIsComparePull"] = true
	ctx.Data["IsDiffCompare"] = true
	renderAttachmentSettings(ctx)

	var (
		repo        = ctx.Repo.Repository
		attachments []string
	)

	headUser, headRepo, headGitRepo, prInfo, baseBranch, headBranch := ParseCompareInfo(ctx)
	if ctx.Written() {
		return
	}

	patch, err := headGitRepo.GetPatch(prInfo.MergeBase, headBranch)
	if err != nil {
		ctx.Handle(500, "GetPatch", err)
		return
	}

	labelIDs, milestoneID, assigneeID := ValidateRepoMetas(ctx, form)
	if ctx.Written() {
		return
	}

	if setting.AttachmentEnabled {
		attachments = form.Attachments
	}

	if ctx.HasError() {
		ctx.HTML(200, COMPARE_PULL)
		return
	}

	pullIssue := &models.Issue{
		RepoID:      repo.ID,
		Index:       repo.NextIssueIndex(),
		Name:        form.Title,
		PosterID:    ctx.User.Id,
		Poster:      ctx.User,
		MilestoneID: milestoneID,
		AssigneeID:  assigneeID,
		IsPull:      true,
		Content:     form.Content,
	}
	pullRequest := &models.PullRequest{
		HeadRepoID:   headRepo.ID,
		BaseRepoID:   repo.ID,
		HeadUserName: headUser.Name,
		HeadBranch:   headBranch,
		BaseBranch:   baseBranch,
		HeadRepo:     headRepo,
		BaseRepo:     repo,
		MergeBase:    prInfo.MergeBase,
		Type:         models.PULL_REQUEST_GOGS,
	}
	if err := models.NewPullRequest(repo, pullIssue, labelIDs, attachments, pullRequest, patch); err != nil {
		ctx.Handle(500, "NewPullRequest", err)
		return
	} else if err := pullRequest.PushToBaseRepo(); err != nil {
		ctx.Handle(500, "PushToBaseRepo", err)
		return
	} else if err := MailWatchersAndMentions(ctx, pullIssue); err != nil {
		ctx.Handle(500, "MailWatchersAndMentions", err)
		return
	}

	log.Trace("Pull request created: %d/%d", repo.ID, pullIssue.ID)
	ctx.Redirect(ctx.Repo.RepoLink + "/pulls/" + com.ToStr(pullIssue.Index))
}

func TriggerTask(ctx *context.Context) {
	branch := ctx.Query("branch")
	secret := ctx.Query("secret")
	if len(branch) == 0 || len(secret) == 0 {
		ctx.Error(404)
		log.Trace("TriggerTask: branch or secret is empty")
		return
	}
	owner, repo := parseOwnerAndRepo(ctx)
	if ctx.Written() {
		return
	}
	if secret != base.EncodeMD5(owner.Salt) {
		ctx.Error(404)
		log.Trace("TriggerTask [%s/%s]: invalid secret", owner.Name, repo.Name)
		return
	}

	log.Trace("TriggerTask [%d].(new request): %s", repo.ID, branch)

	go models.HookQueue.Add(repo.ID)
	go models.AddTestPullRequestTask(repo.ID, branch)
	ctx.Status(202)
}
