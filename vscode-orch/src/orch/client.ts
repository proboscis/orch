import { execFile } from 'child_process';
import { promisify } from 'util';
import * as vscode from 'vscode';
import { OrchConfig } from '../config';

const execFileAsync = promisify(execFile);

export interface IssueInfo {
  id: string;
  title: string;
  summary?: string;
  status: string;
  path: string;
  runs?: RunSummary[];
}

export interface RunSummary {
  run_id: string;
  status: string;
}

export interface RunInfo {
  issue_id: string;
  issue_status?: string;
  run_id: string;
  short_id: string;
  agent?: string;
  status: string;
  updated_at: string;
  updated_ago: string;
  started_at: string;
  pr_url?: string;
  branch?: string;
  worktree_path?: string;
  tmux_session?: string;
}

export interface RunResult {
  issue_id: string;
  run_id: string;
  status: string;
  branch?: string;
  worktree_path?: string;
  tmux_session?: string;
}

interface IssuesResponse {
  ok: boolean;
  issues: IssueInfo[];
}

interface RunsResponse {
  ok: boolean;
  items: RunInfo[];
}

interface CommandResult {
  ok: boolean;
  issue_id: string;
  run_id: string;
  status: string;
  branch?: string;
  worktree_path?: string;
  tmux_session?: string;
  error?: string;
}

export class OrchClient {
  private issuesCache?: { items: IssueInfo[]; fetchedAt: number };
  private runsCache?: { items: RunInfo[]; fetchedAt: number };
  private issuesInFlight?: Promise<IssueInfo[]>;
  private runsInFlight?: Promise<RunInfo[]>;

  constructor(private readonly config: OrchConfig) {}

  invalidateIssues(): void {
    this.issuesCache = undefined;
  }

  invalidateRuns(): void {
    this.runsCache = undefined;
  }

  async listIssues(includeResolved: boolean): Promise<IssueInfo[]> {
    const cache = this.issuesCache;
    if (cache && this.isCacheFresh(cache.fetchedAt)) {
      return filterIssues(cache.items, includeResolved);
    }

    if (this.issuesInFlight) {
      const items = await this.issuesInFlight;
      return filterIssues(items, includeResolved);
    }

    this.issuesInFlight = this.fetchIssues();
    try {
      const items = await this.issuesInFlight;
      return filterIssues(items, includeResolved);
    } finally {
      this.issuesInFlight = undefined;
    }
  }

  async listRuns(statusFilter: string[]): Promise<RunInfo[]> {
    const cache = this.runsCache;
    if (cache && this.isCacheFresh(cache.fetchedAt)) {
      return filterRuns(cache.items, statusFilter);
    }

    if (this.runsInFlight) {
      const items = await this.runsInFlight;
      return filterRuns(items, statusFilter);
    }

    this.runsInFlight = this.fetchRuns();
    try {
      const items = await this.runsInFlight;
      return filterRuns(items, statusFilter);
    } finally {
      this.runsInFlight = undefined;
    }
  }

  async startRun(issueId: string, agent: string): Promise<RunResult> {
    const result = await this.execOrchJson<CommandResult>([
      'run',
      issueId,
      '--agent',
      agent
    ]);
    if (!result.ok) {
      throw new Error(result.error || 'Failed to start run');
    }
    return result;
  }

  async continueRun(issueId: string, branch: string, agent: string): Promise<RunResult> {
    const result = await this.execOrchJson<CommandResult>([
      'continue',
      '--branch',
      branch,
      '--issue',
      issueId,
      '--agent',
      agent
    ]);
    if (!result.ok) {
      throw new Error(result.error || 'Failed to continue run');
    }
    return result;
  }

  async stopRun(runRef: string): Promise<void> {
    await this.execOrch(['stop', runRef]);
  }

  async resolveRun(runRef: string): Promise<void> {
    await this.execOrch(['resolve', runRef]);
  }

  async listBranches(issueId: string): Promise<string[]> {
    const workspaceRoot = this.config.getWorkspaceRoot();
    if (!workspaceRoot) {
      return [];
    }

    const refPath = `refs/heads/issue/${issueId}/`;
    try {
      const { stdout } = await execFileAsync('git', ['for-each-ref', '--format=%(refname:short)', refPath], {
        cwd: workspaceRoot
      });
      return stdout
        .split(/\r?\n/)
        .map((line) => line.trim())
        .filter((line) => line.length > 0);
    } catch (error) {
      console.warn('orch: failed to list branches', error);
      return [];
    }
  }

  private async fetchIssues(): Promise<IssueInfo[]> {
    const data = await this.execOrchJson<IssuesResponse>(['issue', 'list']);
    if (!data.ok) {
      throw new Error('Failed to list issues');
    }
    this.issuesCache = { items: data.issues || [], fetchedAt: Date.now() };
    return this.issuesCache.items;
  }

  private async fetchRuns(): Promise<RunInfo[]> {
    const data = await this.execOrchJson<RunsResponse>(['ps']);
    if (!data.ok) {
      throw new Error('Failed to list runs');
    }
    this.runsCache = { items: data.items || [], fetchedAt: Date.now() };
    return this.runsCache.items;
  }

  private isCacheFresh(fetchedAt: number): boolean {
    const ttl = Math.max(1000, this.config.getRefreshIntervalMs());
    return Date.now() - fetchedAt < ttl;
  }

  private async execOrch(args: string[]): Promise<void> {
    const scopeArgs = this.buildProjectScopeArgs();
    const workspaceRoot = this.config.getWorkspaceRoot();
    try {
      await execFileAsync('orch', [...scopeArgs, ...args], {
        cwd: workspaceRoot
      });
    } catch (error) {
      const message = formatExecError(error, 'orch');
      vscode.window.showErrorMessage(message);
      throw error;
    }
  }

  private async execOrchJson<T>(args: string[]): Promise<T> {
    const scopeArgs = this.buildProjectScopeArgs();
    const workspaceRoot = this.config.getWorkspaceRoot();
    const commandArgs = ['--json', ...scopeArgs, ...args];
    try {
      const { stdout } = await execFileAsync('orch', commandArgs, {
        cwd: workspaceRoot
      });
      return JSON.parse(stdout) as T;
    } catch (error) {
      const message = formatExecError(error, 'orch');
      vscode.window.showErrorMessage(message);
      throw error;
    }
  }

  private buildProjectScopeArgs(): string[] {
    const projectRoot = this.config.getWorkspaceRoot();
    if (projectRoot) {
      return ['--project-root', projectRoot];
    }
    return [];
  }
}

function filterIssues(items: IssueInfo[], includeResolved: boolean): IssueInfo[] {
  if (includeResolved) {
    return items;
  }
  return items.filter((issue) => issue.status === 'open');
}

function filterRuns(items: RunInfo[], statusFilter: string[]): RunInfo[] {
  if (statusFilter.length === 0) {
    return items;
  }
  const allowed = new Set(statusFilter);
  return items.filter((run) => allowed.has(run.status));
}

function formatExecError(error: unknown, toolName: string): string {
  if (!error || typeof error !== 'object') {
    return `Failed to run ${toolName}`;
  }

  const err = error as { message?: string; stderr?: string };
  const message = err.stderr || err.message;
  if (message) {
    return message.toString().trim();
  }
  return `Failed to run ${toolName}`;
}
