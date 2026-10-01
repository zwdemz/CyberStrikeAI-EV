'use strict';

/**
 * Evaluate a PR using repository IDs and branch names, without reading its code.
 * @param {object} pullRequest GitHub pull request metadata.
 * @param {number} repositoryId Immutable ID of the receiving repository.
 * @returns {{allowed: boolean, reason: string}} Fixed, safe-to-log decision; malformed inputs fail closed.
 */
function evaluateRoute(pullRequest, repositoryId) {
  const base = pullRequest?.base;
  const head = pullRequest?.head;
  if (!Number.isSafeInteger(repositoryId) || repositoryId <= 0 ||
      base?.repo?.id !== repositoryId || !Number.isSafeInteger(head?.repo?.id) ||
      head.repo.id <= 0 || typeof head.ref !== 'string' || !head.ref) {
    return {allowed: false, reason: 'Missing or inconsistent pull request metadata.'};
  }
  const sameRepository = head.repo.id === repositoryId;
  if (base.ref === 'main') {
    return sameRepository && head.ref === 'dev'
      ? {allowed: true, reason: 'Release pull request from this repository dev branch.'}
      : {allowed: false, reason: 'main accepts only this repository dev branch. Retarget work to dev.'};
  }
  if (base.ref === 'dev') {
    return sameRepository && ['dev', 'main'].includes(head.ref)
      ? {allowed: false, reason: 'Use a separate work branch for changes targeting dev.'}
      : {allowed: true, reason: 'Work branch pull request targets dev.'};
  }
  return {allowed: false, reason: 'Work pull requests must target dev; releases must go from dev to main.'};
}

/**
 * Publish required commit statuses on the PR head SHA from a trusted workflow_run job.
 * Aggregate open PRs sharing that SHA and target so another PR cannot supply a
 * passing result for an invalid route. API failures propagate and do not pass a gate.
 * @param {object} dependencies GitHub API client, workflow context and logging core.
 * @returns {Promise<number>} Number of statuses published (zero for stale/closed PRs).
 */
async function publishRoutes({github, context, core}) {
  const run = context.payload.workflow_run;
  const repositoryId = context.payload.repository?.id;
  if (context.eventName !== 'workflow_run' || run?.event !== 'pull_request' ||
      !/^[0-9a-f]{40}$/.test(run?.head_sha || '') ||
      !Number.isSafeInteger(repositoryId)) {
    throw new Error('Unexpected route check event.');
  }
  const repository = context.repo;
  // Fetch live metadata instead of trusting an old run's PR target or head ref.
  const pulls = await github.paginate(github.rest.pulls.list, {
    ...repository, state: 'open', per_page: 100,
  });
  const current = pulls.filter(pull => pull.head?.sha === run.head_sha);
  if (current.length === 0) {
    core.info('No open pull request at this revision; ignoring stale completion.');
    return 0;
  }
  const groups = new Map();
  for (const pull of current) {
    const target = ['dev', 'main'].includes(pull.base?.ref) ? pull.base.ref : 'invalid';
    if (!groups.has(target)) groups.set(target, []);
    groups.get(target).push(evaluateRoute(pull, repositoryId));
  }
  for (const [target, decisions] of groups) {
    const blocked = decisions.find(decision => !decision.allowed);
    const summary = blocked?.reason || decisions[0].reason;
    // A workflow_run CheckRun isn't an eligible required workflow job. Commit
    // statuses are independent of that event and remain bound to this exact SHA.
    await github.rest.repos.createCommitStatus({
      ...repository, context: `pr-route/${target}`, sha: run.head_sha,
      state: blocked ? 'failure' : 'success', description: summary,
    });
  }
  return groups.size;
}

module.exports = {evaluateRoute, publishRoutes};
