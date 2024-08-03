#!/bin/sh
cat logs_view.html | minify --type html | head -c-6 > logs_view.min.html
