import { inject } from 'vue';
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
  // Reads a JSONL stream, yielding chunks of objects, and returning any remaining bytes
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
  return buffer;
}

export class TestDataController {
  @memoPromise(10000)
  async getProjectsList(): Promise<Array<Project>> {
    return (await fetchObject('/api/v1/projects', 'projects list')).projects;
  }

  @memoPromise(500)
  async getProjectRuns(project: string) {
    return (await fetchObject(`/api/v1/projects/${project}/runs`, 'project runs')).runs;
  }

  @memoPromise(10000)
  async getTestRunPlan(project: string, run: string): Promise<RunPlan> {
    return await fetchObject(`/api/v1/projects/${project}/runs/${run}/plan`, 'test run plan');
  }

  getTestStatusSummary(project: string, run: string) {
    return fetchObjects(
      `/api/v1/projects/${project}/runs/${run}/status_summary`,
      'test run status summary'
    );
  }

  async getTestStatusChunk(
    project: string,
    run: string,
    status_id: string,
    offset: number
  ): Promise<any> {
    const res = await fetch(`/api/v1/projects/${project}/runs/${run}/status_stream/${status_id}`);
  }

  subscribeTestStatusUpdates(project: string, run: string, callback: (_: any[]) => void) {
    (async () => {
      const statuses = await this.getTestStatusSummary(project, run);
      while (true) {
        const chunk = await statuses.next();
        if (chunk.done) break;
        callback(chunk.value);
      }
    })();
    return () => {};
  }
}

export class TestTestDataController {
  async getProjectsList() {
    return [
      {
        id: 'project1',
        name: 'Project 1',
        runs: [
          {
            id: 'run1',
            name: 'Run 1',
            pretty_age: '2h ago',
            created_at: '2023-10-01T12:00:00Z'
          }
        ]
      }
    ];
  }

  async getProjectRuns(project: string) {
    return [
      { id: 'run1', name: 'Run 1', pretty_age: '2h ago', created_at: '2023-10-01T12:00:00Z' }
    ];
  }

  async getTestRunPlan(project: string, run: string) {
    return {
      worker_count: 1,
      groups: {
        test_thing: [
          {
            id: 'test_thing.py::test_stdout[x86]',
            name: 'test_stdout',
            params: {
              arch: 'x86'
            }
          },
          {
            id: 'test_thing.py::test_stdout[x86_64]',
            name: 'test_stdout',
            params: {
              arch: 'x86_64'
            }
          },
          {
            id: 'test_thing.py::test_stdout[arm]',
            name: 'test_stdout',
            params: {
              arch: 'arm'
            }
          },
          {
            id: 'test_thing.py::test_stdout[aarch64]',
            name: 'test_stdout',
            params: {
              arch: 'aarch64'
            }
          },
          {
            id: 'test_thing.py::test_thing_1[x86]',
            name: 'test_thing_1',
            params: {
              arch: 'x86'
            }
          },
          {
            id: 'test_thing.py::test_thing_1[x86_64]',
            name: 'test_thing_1',
            params: {
              arch: 'x86_64'
            }
          },
          {
            id: 'test_thing.py::test_thing_1[arm]',
            name: 'test_thing_1',
            params: {
              arch: 'arm'
            }
          },
          {
            id: 'test_thing.py::test_thing_1[aarch64]',
            name: 'test_thing_1',
            params: {
              arch: 'aarch64'
            }
          },
          {
            id: 'test_thing.py::test_thing_2[x86]',
            name: 'test_thing_2',
            params: {
              arch: 'x86'
            }
          },
          {
            id: 'test_thing.py::test_thing_2[x86_64]',
            name: 'test_thing_2',
            params: {
              arch: 'x86_64'
            }
          },
          {
            id: 'test_thing.py::test_thing_2[arm]',
            name: 'test_thing_2',
            params: {
              arch: 'arm'
            }
          },
          {
            id: 'test_thing.py::test_thing_2[aarch64]',
            name: 'test_thing_2',
            params: {
              arch: 'aarch64'
            }
          },
          {
            id: 'test_thing.py::test_progress_bar[x86]',
            name: 'test_progress_bar',
            params: {
              arch: 'x86'
            }
          },
          {
            id: 'test_thing.py::test_progress_bar[x86_64]',
            name: 'test_progress_bar',
            params: {
              arch: 'x86_64'
            }
          },
          {
            id: 'test_thing.py::test_progress_bar[arm]',
            name: 'test_progress_bar',
            params: {
              arch: 'arm'
            }
          },
          {
            id: 'test_thing.py::test_progress_bar[aarch64]',
            name: 'test_progress_bar',
            params: {
              arch: 'aarch64'
            }
          },
          {
            id: 'test_thing.py::test_thing_3[x86-hello]',
            name: 'test_thing_3',
            params: {
              arch: 'x86',
              test: 'hello'
            }
          },
          {
            id: 'test_thing.py::test_thing_3[x86-world]',
            name: 'test_thing_3',
            params: {
              arch: 'x86',
              test: 'world'
            }
          },
          {
            id: 'test_thing.py::test_thing_3[x86_64-hello]',
            name: 'test_thing_3',
            params: {
              arch: 'x86_64',
              test: 'hello'
            }
          },
          {
            id: 'test_thing.py::test_thing_3[x86_64-world]',
            name: 'test_thing_3',
            params: {
              arch: 'x86_64',
              test: 'world'
            }
          },
          {
            id: 'test_thing.py::test_thing_3[arm-hello]',
            name: 'test_thing_3',
            params: {
              arch: 'arm',
              test: 'hello'
            }
          },
          {
            id: 'test_thing.py::test_thing_3[arm-world]',
            name: 'test_thing_3',
            params: {
              arch: 'arm',
              test: 'world'
            }
          },
          {
            id: 'test_thing.py::test_thing_3[aarch64-hello]',
            name: 'test_thing_3',
            params: {
              arch: 'aarch64',
              test: 'hello'
            }
          },
          {
            id: 'test_thing.py::test_thing_3[aarch64-world]',
            name: 'test_thing_3',
            params: {
              arch: 'aarch64',
              test: 'world'
            }
          },
          {
            id: 'test_thing.py::test_failure[x86]',
            name: 'test_failure',
            params: {
              arch: 'x86'
            }
          },
          {
            id: 'test_thing.py::test_failure[x86_64]',
            name: 'test_failure',
            params: {
              arch: 'x86_64'
            }
          },
          {
            id: 'test_thing.py::test_failure[arm]',
            name: 'test_failure',
            params: {
              arch: 'arm'
            }
          },
          {
            id: 'test_thing.py::test_failure[aarch64]',
            name: 'test_failure',
            params: {
              arch: 'aarch64'
            }
          },
          {
            id: 'test_thing.py::test_skip[x86]',
            name: 'test_skip',
            params: {
              arch: 'x86'
            }
          },
          {
            id: 'test_thing.py::test_skip[x86_64]',
            name: 'test_skip',
            params: {
              arch: 'x86_64'
            }
          },
          {
            id: 'test_thing.py::test_skip[arm]',
            name: 'test_skip',
            params: {
              arch: 'arm'
            }
          },
          {
            id: 'test_thing.py::test_skip[aarch64]',
            name: 'test_skip',
            params: {
              arch: 'aarch64'
            }
          }
        ]
      },
      row_params: ['arch']
    };
  }

  getTestStatusUpdates(project: string, run: string, offset: number): Promise<any[]> {
    const results = [
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_stdout[x86]',
        time: 1722604538.1610284
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_stdout[x86]',
        time: 1722604538.1634965
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_stdout[x86]',
        time: 1722604538.165063
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_stdout[x86_64]',
        time: 1722604538.1682065
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_stdout[x86_64]',
        time: 1722604538.16975
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_stdout[x86_64]',
        time: 1722604538.1714013
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_stdout[arm]',
        time: 1722604538.1747594
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_stdout[arm]',
        time: 1722604538.176938
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_stdout[arm]',
        time: 1722604538.1783712
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_stdout[aarch64]',
        time: 1722604538.18167
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_stdout[aarch64]',
        time: 1722604538.1837552
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_stdout[aarch64]',
        time: 1722604538.185176
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_1[x86]',
        time: 1722604538.1881895
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_1[x86]',
        time: 1722604538.1902754
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_1[x86]',
        time: 1722604538.191411
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_1[x86_64]',
        time: 1722604538.193644
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_1[x86_64]',
        time: 1722604538.1964028
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_1[x86_64]',
        time: 1722604538.1978178
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_1[arm]',
        time: 1722604538.201007
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_1[arm]',
        time: 1722604538.204382
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_1[arm]',
        time: 1722604538.2058413
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_1[aarch64]',
        time: 1722604538.2085705
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_1[aarch64]',
        time: 1722604538.2112908
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_1[aarch64]',
        time: 1722604538.212684
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_2[x86]',
        time: 1722604538.2154999
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_2[x86]',
        time: 1722604538.2179115
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_2[x86]',
        time: 1722604538.2193327
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_2[x86_64]',
        time: 1722604538.2221413
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_2[x86_64]',
        time: 1722604538.2246404
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_2[x86_64]',
        time: 1722604538.2260067
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_2[arm]',
        time: 1722604538.2288709
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_2[arm]',
        time: 1722604538.231397
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_2[arm]',
        time: 1722604538.2327733
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_2[aarch64]',
        time: 1722604538.2375357
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_2[aarch64]',
        time: 1722604538.2402735
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_2[aarch64]',
        time: 1722604538.2416759
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_progress_bar[x86]',
        time: 1722604538.2449203
      },
      {
        status: 'progress',
        percentage: 0.0,
        time: 1722604538.2465057,
        test: 'test_thing.py::test_progress_bar[x86]'
      },
      {
        status: 'progress',
        percentage: 0.2,
        time: 1722604538.3682566,
        test: 'test_thing.py::test_progress_bar[x86]'
      },
      {
        status: 'progress',
        percentage: 0.4,
        time: 1722604538.4898887,
        test: 'test_thing.py::test_progress_bar[x86]'
      },
      {
        status: 'progress',
        percentage: 0.6,
        time: 1722604538.6115777,
        test: 'test_thing.py::test_progress_bar[x86]'
      },
      {
        status: 'progress',
        percentage: 0.8,
        time: 1722604538.7333047,
        test: 'test_thing.py::test_progress_bar[x86]'
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_progress_bar[x86]',
        time: 1722604538.8560019
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_progress_bar[x86]',
        time: 1722604538.857381
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_progress_bar[x86_64]',
        time: 1722604538.8608963
      },
      {
        status: 'progress',
        percentage: 0.0,
        time: 1722604538.8623497,
        test: 'test_thing.py::test_progress_bar[x86_64]'
      },
      {
        status: 'progress',
        percentage: 0.2,
        time: 1722604538.983857,
        test: 'test_thing.py::test_progress_bar[x86_64]'
      },
      {
        status: 'progress',
        percentage: 0.4,
        time: 1722604539.105406,
        test: 'test_thing.py::test_progress_bar[x86_64]'
      },
      {
        status: 'progress',
        percentage: 0.6,
        time: 1722604539.2268405,
        test: 'test_thing.py::test_progress_bar[x86_64]'
      },
      {
        status: 'progress',
        percentage: 0.8,
        time: 1722604539.348161,
        test: 'test_thing.py::test_progress_bar[x86_64]'
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_progress_bar[x86_64]',
        time: 1722604539.4698696
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_progress_bar[x86_64]',
        time: 1722604539.470872
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_progress_bar[arm]',
        time: 1722604539.4729545
      },
      {
        status: 'progress',
        percentage: 0.0,
        time: 1722604539.4743173,
        test: 'test_thing.py::test_progress_bar[arm]'
      },
      {
        status: 'progress',
        percentage: 0.2,
        time: 1722604539.5960042,
        test: 'test_thing.py::test_progress_bar[arm]'
      },
      {
        status: 'progress',
        percentage: 0.4,
        time: 1722604539.7180865,
        test: 'test_thing.py::test_progress_bar[arm]'
      },
      {
        status: 'progress',
        percentage: 0.6,
        time: 1722604539.839517,
        test: 'test_thing.py::test_progress_bar[arm]'
      },
      {
        status: 'progress',
        percentage: 0.8,
        time: 1722604539.9608982,
        test: 'test_thing.py::test_progress_bar[arm]'
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_progress_bar[arm]',
        time: 1722604540.082887
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_progress_bar[arm]',
        time: 1722604540.0842285
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_progress_bar[aarch64]',
        time: 1722604540.0869536
      },
      {
        status: 'progress',
        percentage: 0.0,
        time: 1722604540.0882936,
        test: 'test_thing.py::test_progress_bar[aarch64]'
      },
      {
        status: 'progress',
        percentage: 0.2,
        time: 1722604540.209741,
        test: 'test_thing.py::test_progress_bar[aarch64]'
      },
      {
        status: 'progress',
        percentage: 0.4,
        time: 1722604540.3319328,
        test: 'test_thing.py::test_progress_bar[aarch64]'
      },
      {
        status: 'progress',
        percentage: 0.6,
        time: 1722604540.453659,
        test: 'test_thing.py::test_progress_bar[aarch64]'
      },
      {
        status: 'progress',
        percentage: 0.8,
        time: 1722604540.5751216,
        test: 'test_thing.py::test_progress_bar[aarch64]'
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_progress_bar[aarch64]',
        time: 1722604540.6970494
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_progress_bar[aarch64]',
        time: 1722604540.6983583
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[x86-hello]',
        time: 1722604540.700998
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[x86-hello]',
        time: 1722604540.7031279
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[x86-hello]',
        time: 1722604540.7042878
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[x86-world]',
        time: 1722604540.7068112
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[x86-world]',
        time: 1722604540.708608
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[x86-world]',
        time: 1722604540.7097359
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[x86_64-hello]',
        time: 1722604540.7122993
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[x86_64-hello]',
        time: 1722604540.7146437
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[x86_64-hello]',
        time: 1722604540.7158377
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[x86_64-world]',
        time: 1722604540.718484
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[x86_64-world]',
        time: 1722604540.720354
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[x86_64-world]',
        time: 1722604540.7217357
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[arm-hello]',
        time: 1722604540.7244549
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[arm-hello]',
        time: 1722604540.7263267
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[arm-hello]',
        time: 1722604540.7276661
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[arm-world]',
        time: 1722604540.7305324
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[arm-world]',
        time: 1722604540.7325463
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[arm-world]',
        time: 1722604540.7338758
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[aarch64-hello]',
        time: 1722604540.7368155
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[aarch64-hello]',
        time: 1722604540.7388642
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[aarch64-hello]',
        time: 1722604540.740156
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[aarch64-world]',
        time: 1722604540.7429826
      },
      {
        status: 'call',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[aarch64-world]',
        time: 1722604540.7449474
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_thing_3[aarch64-world]',
        time: 1722604540.7468262
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_failure[x86]',
        time: 1722604540.7494771
      },
      {
        status: 'call',
        outcome: 'failed',
        test: 'test_thing.py::test_failure[x86]',
        exception:
          'arch = \'x86\'\n\n    def test_failure(arch):\n>       assert False, "LOLOLOL"\nE       AssertionError: LOLOLOL\nE       assert False\n\ntest_thing.py:42: AssertionError',
        time: 1722604540.8187387
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_failure[x86]',
        time: 1722604540.8197136
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_failure[x86_64]',
        time: 1722604540.821437
      },
      {
        status: 'call',
        outcome: 'failed',
        test: 'test_thing.py::test_failure[x86_64]',
        exception:
          'arch = \'x86_64\'\n\n    def test_failure(arch):\n>       assert False, "LOLOLOL"\nE       AssertionError: LOLOLOL\nE       assert False\n\ntest_thing.py:42: AssertionError',
        time: 1722604540.828004
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_failure[x86_64]',
        time: 1722604540.8290586
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_failure[arm]',
        time: 1722604540.8309033
      },
      {
        status: 'call',
        outcome: 'failed',
        test: 'test_thing.py::test_failure[arm]',
        exception:
          'arch = \'arm\'\n\n    def test_failure(arch):\n>       assert False, "LOLOLOL"\nE       AssertionError: LOLOLOL\nE       assert False\n\ntest_thing.py:42: AssertionError',
        time: 1722604540.8391466
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_failure[arm]',
        time: 1722604540.8402848
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_failure[aarch64]',
        time: 1722604540.8423347
      },
      {
        status: 'call',
        outcome: 'failed',
        test: 'test_thing.py::test_failure[aarch64]',
        exception:
          'arch = \'aarch64\'\n\n    def test_failure(arch):\n>       assert False, "LOLOLOL"\nE       AssertionError: LOLOLOL\nE       assert False\n\ntest_thing.py:42: AssertionError',
        time: 1722604540.8499274
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_failure[aarch64]',
        time: 1722604540.8512006
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_skip[x86]',
        time: 1722604540.8532639
      },
      {
        status: 'call',
        outcome: 'skipped',
        test: 'test_thing.py::test_skip[x86]',
        reason: 'Skipped: why not',
        time: 1722604540.8551805
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_skip[x86]',
        time: 1722604540.8561802
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_skip[x86_64]',
        time: 1722604540.8584256
      },
      {
        status: 'call',
        outcome: 'skipped',
        test: 'test_thing.py::test_skip[x86_64]',
        reason: 'Skipped: why not',
        time: 1722604540.8608575
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_skip[x86_64]',
        time: 1722604540.8618567
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_skip[arm]',
        time: 1722604540.8638904
      },
      {
        status: 'call',
        outcome: 'skipped',
        test: 'test_thing.py::test_skip[arm]',
        reason: 'Skipped: why not',
        time: 1722604540.8661458
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_skip[arm]',
        time: 1722604540.8673308
      },
      {
        status: 'setup',
        outcome: 'passed',
        test: 'test_thing.py::test_skip[aarch64]',
        time: 1722604540.8695636
      },
      {
        status: 'call',
        outcome: 'skipped',
        test: 'test_thing.py::test_skip[aarch64]',
        reason: 'Skipped: why not',
        time: 1722604540.8716333
      },
      {
        status: 'teardown',
        outcome: 'passed',
        test: 'test_thing.py::test_skip[aarch64]',
        time: 1722604540.872589
      }
    ];
    return new Promise((resolve, reject) => {
      if (offset < results.length) {
        setTimeout(() => {
          resolve(results.slice(offset, offset + 1));
        }, 50);
      }
    });
  }

  subscribeTestStatusUpdates(project: string, run: string, callback: (_: any[]) => void) {
    const self = this;
    let offset = 0;
    let canceled = false;
    async function inner_callback(chunk?: any[]) {
      if (canceled) {
        return;
      }
      if (chunk) {
        offset += chunk.length;
        callback(chunk);
      }
      self.getTestStatusUpdates(project, run, offset).then(inner_callback);
    }
    inner_callback();

    return () => {
      canceled = true;
    };
  }
}

export async function useProjectsList(): Promise<Array<Project>> {
  return await inject<TestDataController>('test_data')!.getProjectsList();
}
export async function useProjectRuns(project: string) {
  return await inject<TestDataController>('test_data')!.getProjectRuns(project);
}
export async function useTestRunPlan(project: string, run: string): Promise<RunPlan> {
  return await inject<TestDataController>('test_data')!.getTestRunPlan(project, run);
}
export function useTestLogs(project: string, run: string, test: string) {}
