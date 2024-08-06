#!/bin/sh
cat logs_view.html | minify --type html | head -c-6 > logs_view.min.html
cat tail_logs_view_prefix.html | minify --type html > tail_logs_view_prefix.min.html
