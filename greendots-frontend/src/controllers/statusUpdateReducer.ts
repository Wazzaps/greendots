export default function statusUpdateReducer(status_update: any, test_items: any, test_idx: number) {
  if (status_update.outcome == 'failed' || status_update.outcome == 'error') {
    test_items.value[test_idx].status = 'fail';
    test_items.value[test_idx].progress = 1;
    return;
  } else if (status_update.outcome == 'skipped') {
    test_items.value[test_idx].status = 'skip';
    test_items.value[test_idx].progress = 1;
  } else if (status_update.type == 'start') {
    test_items.value[test_idx].status = 'progress';
    test_items.value[test_idx].progress = 0;
  } else if (status_update.type == 'finish' && status_update.outcome == 'passed') {
    // non-passed outcomes are handled above
    test_items.value[test_idx].status = 'success';
    test_items.value[test_idx].progress = 1;
  } else if (status_update.type == 'progress') {
    const current_status = test_items.value[test_idx].status;
    if (current_status == 'pending' || current_status == 'progress') {
      test_items.value[test_idx].status = 'progress';
      test_items.value[test_idx].progress = status_update.progress;
    }
  } else {
    console.error('Unknown status update type:', status_update);
  }
}
