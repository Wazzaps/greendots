import memoPromise from './memoPromise';

export type Project = {
  id: string;
  name: string;
  runs: Array<Run>;
};

export type Run = {
  id: string;
  name: string;
  pretty_age: string;
  created_at: string;
};

export type RunPlan = {
  worker_count: number;
  groups: { [group: string]: Array<RunPlanTestItem> };
  row_params: Array<string>;
};

export type RunPlanTestItem = {
  id: string;
  name: string;
  params: { [param: string]: string };
};

async function fetchObject(url: string, desc: string = '', options?: RequestInit) {
  const res = await fetch(url, options);
  if (!res.ok) {
    throw new Error(`Failed to fetch ${desc || url}`);
  }
  const obj = await res.json();
  if (obj.error) {
    throw new Error(obj.error);
  }
  return obj;
}

async function* fetchObjects(
  url: string,
  desc: string = '',
  options?: RequestInit,
  buffer: string = ''
) {
  // Reads a JSONL stream, yielding chunks of objects, and returning any remaining bytes and the headers
  const res = await fetch(url, options);
  if (!res.ok) {
    throw new Error(`Failed to fetch ${desc || url}`);
  }
  const reader = res.body!.getReader();
  let done = false;

  while (!done) {
    const { value, done: readerDone } = await reader.read();
    done = readerDone;
    buffer += new TextDecoder().decode(value);

    const lines = buffer.split('\n');
    buffer = lines.pop() || '';

    const chunk = [];
    for (const line of lines) {
      if (line.length === 0) continue;
      chunk.push(JSON.parse(line));
      // force small chunks, didn't seem to help
      // if (chunk.length > 500) {
      //   yield chunk;
      //   chunk = [];
      // }
    }
    if (chunk.length > 0) {
      yield chunk;
    }
  }
  return [buffer, res.headers];
}

function concatBufs(a: Uint8Array, b: Uint8Array): Uint8Array {
  const c = new Uint8Array(a.byteLength + b.byteLength);
  c.set(a, 0);
  c.set(b, a.byteLength);
  return c;
}

function sliceBuf(buf: Uint8Array, start: number): Uint8Array {
  const dst = new Uint8Array(buf.byteLength - start);
  dst.set(buf.subarray(start));
  return dst;
}

export class TestDataFetcher {
  @memoPromise(10000)
  async getProjectsList(): Promise<Array<Project>> {
    return (await fetchObject('/api/v1/projects', 'projects list')).projects;
  }

  @memoPromise(500)
  async getProjectRuns(project: string) {
    return (
      await fetchObject(`/api/v1/projects/${encodeURIComponent(project)}/runs`, 'project runs')
    ).runs;
  }

  @memoPromise(Infinity)
  async getTestRunPlan(project: string, run: string): Promise<RunPlan> {
    return await fetchObject(
      `/api/v1/projects/${encodeURIComponent(project)}/runs/${encodeURIComponent(run)}/plan`,
      'test run plan'
    );
  }

  getTestStatusSummary(project: string, run: string, options?: RequestInit) {
    return fetchObjects(
      `/api/v1/projects/${encodeURIComponent(project)}/runs/${encodeURIComponent(run)}/status_summary`,
      'test run status summary',
      options
    );
  }

  async getTestStatusChunk(
    project: string,
    run: string,
    worker_id: string,
    offset: number
  ): Promise<Uint8Array> {
    const res = await fetch(
      `/api/v1/projects/${encodeURIComponent(project)}/runs/${encodeURIComponent(run)}/status_stream/${encodeURIComponent(worker_id)}`,
      {
        headers: { Range: `bytes=${offset}-` }
      }
    );
    if (!res.ok) {
      throw new Error('Failed to fetch test status chunk');
    }
    if (offset > 0) {
      const start_offset = parseInt(res.headers.get('Content-Range')!.split(' ')[1].split('-')[0]);
      if (start_offset !== offset) {
        throw new Error('Misbehaving server');
      }
    }
    return new Uint8Array(await res.arrayBuffer());
  }

  subscribeTestStatusUpdates(
    project: string,
    run: string,
    callback: (chunk: TestStatusUpdateEvent[]) => void
  ) {
    let done = false;
    const abort_controller = new AbortController();
    (async () => {
      let headers: Headers | null = null;
      const statuses = await this.getTestStatusSummary(project, run, {
        signal: abort_controller.signal
      });
      while (!done) {
        const chunk = await statuses.next();
        if (chunk.done) {
          headers = chunk.value[1];
          break;
        }

        callback(chunk.value);
      }
      callback([{ type: 'summary_done' }]);
      if (!headers) throw new Error('No headers');
      const offsets = headers
        .get('x-end-offset')!
        .split(',')
        .map((it) => parseInt(it));

      const buffers = offsets.map(() => new Uint8Array(0));
      while (!done) {
        // Wait for updated status files
        let res;
        try {
          res = await fetchObject(
          `/api/v1/projects/${encodeURIComponent(project)}/runs/${encodeURIComponent(run)}/status_poll`,
          'test status size updates',
            { body: JSON.stringify(offsets), method: 'POST', signal: abort_controller.signal }
        );
        } catch (e) {
          // Try again in a bit
          await new Promise((resolve) => setTimeout(resolve, 2000 + Math.random() * 1000));
          continue;
        }
        for (const worker_id of res.workers_to_check) {
          const chunk = await this.getTestStatusChunk(project, run, worker_id, offsets[worker_id]);
          buffers[worker_id] = concatBufs(buffers[worker_id], chunk);
          offsets[worker_id] += chunk.byteLength;

          const newline_idx = buffers[worker_id].lastIndexOf(0xa); // '\n' charcode
          if (newline_idx === -1) continue;

          const lines = new TextDecoder()
            .decode(buffers[worker_id].subarray(0, newline_idx) as ArrayBuffer)
            .split('\n');

          callback(lines.map((line) => JSON.parse(line)));
          buffers[worker_id] = sliceBuf(buffers[worker_id], newline_idx + 1);
        }
        // Wait a bit to avoid hammering the server
        await new Promise((resolve) => setTimeout(resolve, 500));
      }
    })();

    // Unsubscribe function
    return () => {
      done = true;
      abort_controller.abort();
    };
  }
}

export type TestStatusUpdateEvent =
  | { type: 'summary_done' }
  | { type: 'start'; test: string }
  | { type: 'progress'; percentage: number }
  | {
      type: 'setup' | 'call' | 'teardown' | 'finish';
      outcome: 'passed' | 'failed' | 'error' | 'skipped';
      test: string;
    };

export type TestItem = {
  id: string;
  group: string;
  name: string;
  params: { [param: string]: string };

  shown: boolean;
  row_idx: number;
  col_idx: number;
  // TODO: Rename to be closer to `outcome`
  status: 'pending' | 'progress' | 'success' | 'fail' | 'skip';
  progress: number;
};

export type Row = {
  params: { [param: string]: string };

  shown: boolean;
};
export type Col = {
  params: { [param: string]: string };

  shown: boolean;
};
export type TestGroup = {
  name: string;
  start: number;
  end: number;

  shown: boolean;
};

export type ProcessedPlan = {
  id: number;
  test_items: TestItem[];
  test_id_to_test_idx: { [_: string]: number };
  rows: Row[];
  cols: Col[];
  test_groups: TestGroup[];
  row_params: string[];
};

export type TestDataProcessorEvent =
  | { type: 'reset' }
  | ({ type: 'plan' } & ProcessedPlan)
  | { type: 'status_update'; test_idx: number }
  | { type: 'surprise' };

export class TestDataProcessor {
  // Inputs
  private project: string | null = null;
  private run: string | null = null;
  // private filter: null | ((_: TestItem) => boolean) = null;

  // State
  private plan: (TestDataProcessorEvent & ProcessedPlan) | null = null;
  private successes_left_for_surprise: number | null = null;
  // private exceptions: Array<string> = [];
  private statusUnsub: (() => void) | null = null;
  private next_plan_id = 0;

  constructor(
    private fetcher: TestDataFetcher,
    private callback: (event: TestDataProcessorEvent) => void
  ) {}

  public setTestRun(project: string, run: string) {
    this.callback({ type: 'reset' });
    this.unsubscribe();
    this.project = project;
    this.run = run;
    // this.exceptions = [];
    this.successes_left_for_surprise = null;

    (async () => {
      const raw_plan = await this.fetcher.getTestRunPlan(project, run);
      // Stale request, ignore
      if (this.project !== project || this.run !== run) return;

      // Arrange test items in a grid
      this.plan = TestDataProcessor.processTestPlan(raw_plan);
      this.plan.id = this.next_plan_id++;
      // TODO: filter/enrich?
      this.callback(this.plan);

      this.statusUnsub = this.fetcher.subscribeTestStatusUpdates(
        project,
        run,
        this.testStatusCallback.bind(this)
      );
    })();
  }

  public unsubscribe() {
    this.statusUnsub?.();
    this.statusUnsub = null;
  }

  private static processTestPlan(raw_plan: RunPlan): TestDataProcessorEvent & ProcessedPlan {
    const rows: Row[] = [];
    const rows_json = [];
    const cols: Col[] = [];
    const cols_json = [];
    const test_items: TestItem[] = [];
    const test_id_to_test_idx: { [_: string]: number } = {};
    const test_groups: TestGroup[] = [];
    for (const group_name in raw_plan.groups) {
      const group = raw_plan.groups[group_name];
      const group_start_idx = cols.length;
      for (const test of group) {
        const row_data: { [_: string]: string } = {};
        const col_data: { [_: string]: string } = {
          group: group_name,
          test_name: test.name
        };
        // Collect row and col data
        for (const row_name of raw_plan.row_params) {
          if (test.params[row_name] !== undefined) {
            row_data[row_name] = test.params[row_name];
          } else {
            row_data[row_name] = 'common';
          }
        }
        for (const param_name in test.params) {
          if (!raw_plan.row_params.includes(param_name)) {
            col_data[param_name] = test.params[param_name];
          }
        }
        // Collect in global rows/cols
        const row_data_json = JSON.stringify(row_data);
        const col_data_json = JSON.stringify(col_data);
        let row_idx = rows_json.indexOf(row_data_json);
        if (row_idx == -1) {
          row_idx = rows_json.length;
          rows_json.push(row_data_json);
          rows.push({ params: row_data, shown: true });
        }
        let col_idx = cols_json.indexOf(col_data_json);
        if (col_idx == -1) {
          col_idx = cols_json.length;
          cols_json.push(col_data_json);
          cols.push({ params: col_data, shown: true });
        }
        // Add test item
        test_id_to_test_idx[test.id] = test_items.length;
        test_items.push({
          id: test.id,
          group: group_name,
          name: test.name,
          params: test.params,

          shown: true,
          row_idx,
          col_idx,
          status: 'pending',
          progress: 0
        });
      }
      const group_end_idx = cols.length;
      test_groups.push({
        name: group_name,
        start: group_start_idx,
        end: group_end_idx,
        shown: true
      });
    }

    return {
      type: 'plan',
      id: 0, // To be filled by caller
      test_items,
      test_id_to_test_idx,
      rows,
      cols,
      test_groups,
      row_params: raw_plan.row_params
    };
  }

  private testStatusCallback(chunk: TestStatusUpdateEvent[]) {
    for (const status_update of chunk) {
      const test_idx: number | undefined =
        this.plan!.test_id_to_test_idx[(status_update as any).test];
      const test: TestItem | undefined = this.plan!.test_items[test_idx];
      const old_status = test?.status;

      // TODO: refactor to be less broken with malicious server
      if (status_update.type == 'summary_done') {
        let new_successes_left_for_surprise = 0;
        for (const item of this.plan!.test_items) {
          if (item.status != 'success') {
            new_successes_left_for_surprise++;
          }
        }
        this.successes_left_for_surprise = new_successes_left_for_surprise;
      } else if (status_update.outcome == 'failed' || status_update.outcome == 'error') {
        // TODO: Collect exceptions
        test.status = 'fail';
        test.progress = 1;
      } else if (status_update.outcome == 'skipped') {
        test.status = 'skip';
        test.progress = 1;
      } else if (status_update.type == 'setup' || status_update.type == 'start') {
        test.status = 'progress';
        test.progress = 0;
      } else if (status_update.type == 'finish' && status_update.outcome == 'passed') {
        // non-passed outcomes are handled above
        test.status = 'success';
        test.progress = 1;
      } else if (status_update.type == 'progress') {
        if (test.status == 'pending' || test.status == 'progress') {
          test.status = 'progress';
          test.progress = status_update.percentage;
        }
      } else if (status_update.type == 'call' || status_update.type == 'teardown') {
        // ignore
      } else {
        console.error('Unknown status update type:', status_update);
        continue;
      }

      if (test_idx !== undefined) {
        this.callback({ type: 'status_update', test_idx });
      }

      // Check for surprise
      if (this.successes_left_for_surprise !== null) {
        const new_status = test?.status;
        if (old_status != 'success' && new_status == 'success') {
          if (this.successes_left_for_surprise > 0) {
            this.successes_left_for_surprise -= 1;
            if (this.successes_left_for_surprise == 0) {
              this.callback({ type: 'surprise' });
            }
          }
        } else if (old_status == 'success' && new_status != 'success') {
          this.successes_left_for_surprise += 1;
        }
      }
    }
  }
}
