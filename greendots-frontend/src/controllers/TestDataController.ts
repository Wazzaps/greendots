import { cyrb53, cyrb53_base36_6chars } from '@/utils/str_hash';
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
      while (!done) {
        try {
          const statuses = await this.getTestStatusSummary(project, run, {
            signal: abort_controller.signal
          });
          while (!done) {
            const chunk = await statuses.next();
            if (chunk.done) {
              headers = chunk.value[1] as Headers | null;
              break;
            }

            if (done) return;
            callback(chunk.value);
          }
          break;
        } catch (e) {
          // Try again in a bit
          await new Promise((resolve) => setTimeout(resolve, 2000 + Math.random() * 1000));
        }
      }

      if (done) return;
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

          if (done) return;
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
      exception?: string;
    };

// Keep this list in sync with the fields in `TestItem`
export const TEST_ITEM_FILTERABLE_FIELDS = [
  'id',
  'group',
  'name',
  'params',
  'status',
  'progress',
  'exception',
  'ex'
];
export type TestItem = {
  id: string;
  group: string;
  name: string;
  params: { [param: string]: string };

  shown: boolean;
  // pre-filter
  _row_idx: number;
  _col_idx: number;
  // post-filter
  row_idx: number;
  col_idx: number;
  // TODO: Rename to be closer to `outcome`
  status: 'pending' | 'progress' | 'success' | 'fail' | 'skip';
  progress: number;
  exception?: string;
  ex?: string; // A short hash of the exception
  ex_color?: string; // A color derived from the exception hash
};

export type Row = {
  params: { [param: string]: string };

  shown: boolean;
  _shown_tests: number; // Number of tests shown in this row
};
export type Col = {
  params: { [param: string]: string };

  shown: boolean;
  _shown_tests: number; // Number of tests shown in this col
};
export type TestGroup = {
  name: string;
  start: number;
  end: number;
};
export type ExceptionData = {
  id: string;
  text: string;
  long_text: string;
  color: string;
  count: number;
};
export type ExceptionMap = { [id: string]: ExceptionData };

export type ProcessedPlan = {
  id: number;
  test_items: TestItem[];
  test_id_to_test_idx: { [_: string]: number };
  rows: Row[];
  cols: Col[];
  test_groups: TestGroup[];
  row_params: string[];
  test_counts_by_status: TestStatusCounts;
};

export type TestStatusCounts = {
  pending: number;
  progress: number;
  success: number;
  fail: number;
  skip: number;
};
const DEFAULT_TEST_STATUS_COUNTS: TestStatusCounts = {
  pending: 0,
  progress: 0,
  success: 0,
  fail: 0,
  skip: 0
};

export type TestDataProcessorEvent =
  | { type: 'reset' }
  | ({ type: 'plan' } & ProcessedPlan)
  | { type: 'status_update'; test_idx: number }
  | { type: 'exception_list'; exceptions: ExceptionMap }
  | { type: 'tests_done'; test_counts_by_status: TestStatusCounts }
  | { type: 'first_fail'; test_id: string };

export class TestDataProcessor {
  // Inputs
  private project: string | null = null;
  private run: string | null = null;
  private filter: null | ((_: TestItem) => boolean) = null;

  // State
  private plan: (TestDataProcessorEvent & ProcessedPlan) | null = null;
  private filteredPlan: (TestDataProcessorEvent & ProcessedPlan) | null = null;
  private seen_whole_summary = false;
  private seen_first_fail = false;
  private exceptions: ExceptionMap = {};
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
    this.exceptions = {};
    this.seen_whole_summary = false;
    this.seen_first_fail = false;

    (async () => {
      const raw_plan = await this.fetcher.getTestRunPlan(project, run);
      // Stale request, ignore
      if (this.project !== project || this.run !== run) return;

      // Arrange test items in a grid
      this.plan = TestDataProcessor.processTestPlan(raw_plan);
      this.plan.id = this.next_plan_id++;
      this.filteredPlan = this.plan;
      this.calculateTestGroups();
      this.applyFilter();
      this.callback(this.filteredPlan!);

      this.statusUnsub = this.fetcher.subscribeTestStatusUpdates(
        project,
        run,
        this.testStatusCallback.bind(this)
      );
    })();
  }

  public setFilter(filter: null | ((_: TestItem) => boolean)) {
    this.filter = filter;
    if (this.applyFilter()) {
      this.callback(this.filteredPlan!);
    }
  }

  public unsubscribe() {
    this.statusUnsub?.();
    this.statusUnsub = null;
  }

  private applyFilter(test_list?: TestItem[]): boolean {
    let changed = false;
    if (this.plan) {
      for (const test of test_list || this.plan.test_items) {
        const prev_shown = test.shown;
        test.shown = this.filter ? this.filter(test) : true;
        if (prev_shown != test.shown) {
          // console.log('test.shown changed:', test);
          changed = true;
          // console.log('row', test._row_idx, 'col', test._col_idx, 'delta', test.shown ? 1 : -1);
          const row = this.plan.rows[test._row_idx];
          const col = this.plan.cols[test._col_idx];
          row._shown_tests += test.shown ? 1 : -1;
          col._shown_tests += test.shown ? 1 : -1;
          if (row._shown_tests < 0) {
            console.error(
              `Test filtering broken, row#${test._row_idx} has negative shown test count: ${row._shown_tests}`,
              this.plan
            );
          }
          if (col._shown_tests < 0) {
            console.error(
              `Test filtering broken, col#${test._col_idx} has negative shown test count: ${col._shown_tests}`,
              this.plan
            );
          }
        }
      }
      for (const row of this.plan.rows) {
        const prev_shown = row.shown;
        row.shown = row._shown_tests > 0;
        if (prev_shown != row.shown) {
          // console.log('row.shown changed:', row);
          changed = true;
        }
      }
      for (const col of this.plan.cols) {
        const prev_shown = col.shown;
        col.shown = col._shown_tests > 0;
        if (prev_shown != col.shown) {
          // console.log('col.shown changed:', col);
          changed = true;
        }
      }
      if (changed) {
        console.log('Filter results changed, notifying component');
        this.filteredPlan = this.plan;
        const row_idx_map: Map<number, number> = new Map();
        const col_idx_map: Map<number, number> = new Map();
        this.filteredPlan = {
          type: 'plan',
          id: this.next_plan_id++,
          test_id_to_test_idx: {},
          rows: filter_and_make_idx_map(this.plan.rows, (row) => row.shown, row_idx_map),
          cols: filter_and_make_idx_map(this.plan.cols, (col) => col.shown, col_idx_map),
          test_items: this.plan.test_items.filter((test) => test.shown),
          test_groups: [],
          row_params: this.plan.row_params,
          test_counts_by_status: this.plan.test_counts_by_status
        };
        for (const [idx, test] of this.plan.test_items.entries()) {
          if (test.shown) {
            test.row_idx = row_idx_map.get(test._row_idx)!;
            test.col_idx = col_idx_map.get(test._col_idx)!;
            this.filteredPlan.test_id_to_test_idx[test.id] = idx;
          }
        }
        this.calculateTestGroups();
      }
    } else {
      this.filteredPlan = null;
    }
    return changed;
  }

  private calculateTestGroups() {
    if (this.filteredPlan == null) {
      return;
    }
    let current_group = this.filteredPlan.cols[0]?.params.group;
    let group_start = 0;
    for (const [idx, col] of this.filteredPlan.cols.entries()) {
      if (col.params.group !== current_group) {
        this.filteredPlan.test_groups.push({
          name: current_group,
          start: group_start,
          end: idx
        });
        current_group = col.params.group;
        group_start = idx;
      }
    }
    if (this.filteredPlan.cols.length > 0) {
      this.filteredPlan.test_groups.push({
        name: current_group,
        start: group_start,
        end: this.filteredPlan.cols.length
      });
    }
  }

  private static processTestPlan(raw_plan: RunPlan): TestDataProcessorEvent & ProcessedPlan {
    const rows: Row[] = [];
    const rows_json = [];
    const cols: Col[] = [];
    const cols_json = [];
    const test_items: TestItem[] = [];
    const test_id_to_test_idx: { [_: string]: number } = {};
    const test_counts_by_status = { ...DEFAULT_TEST_STATUS_COUNTS };
    for (const group_name in raw_plan.groups) {
      const group = raw_plan.groups[group_name];
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
          rows.push({ params: row_data, shown: true, _shown_tests: 1 });
        } else {
          rows[row_idx]._shown_tests += 1;
        }
        let col_idx = cols_json.indexOf(col_data_json);
        if (col_idx == -1) {
          col_idx = cols_json.length;
          cols_json.push(col_data_json);
          cols.push({ params: col_data, shown: true, _shown_tests: 1 });
        } else {
          cols[col_idx]._shown_tests += 1;
        }
        // Add test item
        test_id_to_test_idx[test.id] = test_items.length;
        test_counts_by_status.pending += 1;
        test_items.push({
          id: test.id,
          group: group_name,
          name: test.name,
          params: test.params,

          shown: true,
          _row_idx: row_idx,
          _col_idx: col_idx,
          row_idx,
          col_idx,
          status: 'pending',
          progress: 0
        });
      }
    }

    return {
      type: 'plan',
      id: 0, // To be filled by caller
      test_items,
      test_id_to_test_idx,
      rows,
      cols,
      test_groups: [], // To be filled by filter logic
      row_params: raw_plan.row_params,
      test_counts_by_status
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
        this.seen_whole_summary = true;
        if (this.applyFilter()) {
          this.callback(this.filteredPlan!);
        }
      } else if (
        (status_update as any).outcome == 'failed' ||
        (status_update as any).outcome == 'error'
      ) {
        test.status = 'fail';
        test.progress = 1;
        const new_exception = (status_update as any).exception as string | undefined;
        // Use the last line as the exception key
        test.exception = new_exception ? new_exception.split('\n').pop()?.trim() : undefined;

        let ex_changed = false;
        if (test.ex !== undefined) {
          this.exceptions[test.ex].count -= 1;
          if (this.exceptions[test.ex].count == 0) {
            delete this.exceptions[test.ex];
          }
          ex_changed = true;
        }
        test.ex = test.exception ? cyrb53_base36_6chars(test.exception) : undefined;
        test.ex_color = test.ex ? ex_hash_color(test.ex) : undefined;
        if (test.ex) {
          if (this.exceptions[test.ex] === undefined) {
            this.exceptions[test.ex] = {
              id: test.ex,
              text: test.exception!,
              // Also store (one of the) the full exception texts
              long_text: (status_update as any).exception,
              color: test.ex_color!,
              count: 1
            };
          } else {
            this.exceptions[test.ex].count += 1;
          }
          ex_changed = true;
        }

        if (ex_changed) {
          this.callback({ type: 'exception_list', exceptions: this.exceptions });
        }
        if (this.seen_whole_summary && !this.seen_first_fail) {
          this.seen_first_fail = true;
          this.callback({ type: 'first_fail', test_id: test.id });
        }
      } else if ((status_update as any).outcome == 'skipped') {
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
        test.status = 'progress';
        test.progress = status_update.percentage;
      } else if (status_update.type == 'call' || status_update.type == 'teardown') {
        // ignore
      } else {
        console.error('Unknown status update type:', status_update);
        continue;
      }

      if (test_idx !== undefined) {
        if (this.seen_whole_summary) {
          this.applyFilter([test]);
        }
        this.callback({ type: 'status_update', test_idx });
      }

      if (test) {
        // Update test status counters
        const was_done =
          this.plan!.test_counts_by_status.pending == 0 &&
          this.plan!.test_counts_by_status.progress == 0;
        this.plan!.test_counts_by_status[old_status] -= 1;
        this.plan!.test_counts_by_status[test.status] += 1;
        const is_done =
          this.plan!.test_counts_by_status.pending == 0 &&
          this.plan!.test_counts_by_status.progress == 0;
        if (this.plan!.test_counts_by_status[old_status] < 0) {
          console.error('Test status counter underflow:', this.plan!.test_counts_by_status);
        }

        // Check for tests_done, but only if we've already past the summary
        if (!was_done && is_done && this.seen_whole_summary) {
          this.callback({
            type: 'tests_done',
            test_counts_by_status: this.plan!.test_counts_by_status
          });
        }
      }
    }
  }
}

function filter_and_make_idx_map<T>(
  items: Array<T>,
  filter: (item: T) => boolean,
  out_idx_map: Map<number, number>
): Array<T> {
  const filtered_items = [];
  for (const [idx, item] of items.entries()) {
    if (filter(item)) {
      out_idx_map.set(idx, filtered_items.length);
      filtered_items.push(item);
    }
  }
  return filtered_items;
}

export function ex_hash_color(str: string, seed: number = 0) {
  const hash = cyrb53(str, seed);
  // Had a fancy hue-based calculation here, but a plain random color + threshold looked better
  const r = (hash >> 16) & 0xff;
  let g = (hash >> 8) & 0xff;
  const b = hash & 0xff;
  if (g / r > 1.3 && g / b > 1.3) {
    g = (g * 0.5) | 0;
  }
  return '#' + ((r << 16) | (g << 8) | b).toString(16).padStart(6, '0');
}
