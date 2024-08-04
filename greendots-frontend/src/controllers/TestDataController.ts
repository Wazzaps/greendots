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

export class TestDataController {
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

  @memoPromise(10000)
  async getTestRunPlan(project: string, run: string): Promise<RunPlan> {
    return await fetchObject(
      `/api/v1/projects/${encodeURIComponent(project)}/runs/${encodeURIComponent(run)}/plan`,
      'test run plan'
    );
  }

  getTestStatusSummary(project: string, run: string) {
    return fetchObjects(
      `/api/v1/projects/${encodeURIComponent(project)}/runs/${encodeURIComponent(run)}/status_summary`,
      'test run status summary'
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

  subscribeTestStatusUpdates(project: string, run: string, callback: (_: any[]) => void) {
    (async () => {
      let headers: Headers | null = null;
      const statuses = await this.getTestStatusSummary(project, run);
      while (true) {
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
      while (true) {
        // Wait for updated status files
        const res = await fetchObject(
          `/api/v1/projects/${encodeURIComponent(project)}/runs/${encodeURIComponent(run)}/status_poll`,
          'test status size updates',
          { body: JSON.stringify(offsets), method: 'POST' }
        );
        if (res.workers_to_check.length === 0) {
          // Server is telling us to chill
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
    return () => {};
  }
}
