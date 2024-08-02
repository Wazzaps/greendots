export default function memoPromise(
  cache_timeout: number = 0,
  cache_nulls: boolean = true,
  cache_errors: boolean = false
) {
  const cache: { [_: string]: any } = {};
  let cache_clean_interval: number | null = null;

  function cleanCache() {
    const now = Date.now();
    for (const key in cache) {
      if (now - cache[key].timestamp > cache_timeout && !cache[key].in_progress) {
        delete cache[key];
      }
    }
    if (Object.keys(cache).length === 0 && cache_clean_interval !== null) {
      clearInterval(cache_clean_interval);
      cache_clean_interval = null;
    }
  }

  return function (target: any, propertyKey: string, descriptor: PropertyDescriptor) {
    const originalMethod = descriptor.value;
    descriptor.value = async function (...args: any[]) {
      const now = Date.now();
      // Iterate over cache, find matching entry
      const key = JSON.stringify(args);
      const cache_entry = cache[key];
      if (cache_entry && (now - cache_entry.timestamp < cache_timeout || cache_entry.in_progress)) {
        if (cache_entry.in_progress) {
          // Register to be notified when the promise is resolved
          const promise = new Promise((resolve, reject) => {
            cache_entry.resolve_funcs.push(resolve);
            cache_entry.reject_funcs.push(reject);
          });
          return promise;
        } else {
          // Return cached value
          if (cache_entry.error) {
            return Promise.reject(cache_entry.error);
          } else {
            return Promise.resolve(cache_entry.value);
          }
        }
      } else {
        // Create new cache entry
        const resolve_funcs: Array<(_: any) => void> = [];
        const reject_funcs: Array<(_: any) => void> = [];
        const promise = new Promise((resolve, reject) => {
          resolve_funcs.push(resolve);
          reject_funcs.push(reject);
        });
        cache[key] = {
          timestamp: now,
          in_progress: true,
          resolve_funcs,
          reject_funcs
        };

        // Call original method
        originalMethod
          .apply(this, args)
          .then((value: any) => {
            cache[key].resolve_funcs.forEach((resolve_func: (_: any) => void) => {
              resolve_func(value);
            });
            if (cache_timeout === 0 || (!cache_nulls && value === null)) {
              delete cache[key];
            } else {
              cache[key].in_progress = false;
              cache[key].value = value;
            }
          })
          .catch((error: any) => {
            cache[key].reject_funcs.forEach((reject_func: (_: any) => void) => {
              reject_func(error);
            });
            if (cache_timeout === 0 || !cache_errors) {
              delete cache[key];
            } else {
              cache[key].in_progress = false;
              cache[key].error = error;
            }
          });

        // Init cache cleaning
        if (cache_clean_interval === null) {
          cache_clean_interval = setInterval(cleanCache, 1000);
        }

        return promise;
      }
    };
  };
}
