# Renders jobs_record.png from jobs_record.txt.
#
# DESIGN NOTES
#
# Two stacked panels sharing one time axis, deliberately NOT a dual-axis chart:
# postings and source count differ by three orders of magnitude, so twin y-scales
# invite false visual correlation and squash the source line into a meaningless
# flat streak. Separate panels state each honestly.
#
# Colours come from a CVD-validated categorical palette (blue slot 1, orange
# slot 2): worst-pair ΔE 24.7 under protanopia and 33.6 for normal vision. Chrome
# is deliberately recessive, hairline horizontal grid, two spines, muted ticks --
# so the data carries the contrast. Both y-axes start at zero, because a truncated
# axis exaggerates every change and this series has enough real drama already.
#
# GAPS MUST BREAK THE LINE. 550 days recorded no posting count. `set datafile
# missing` is NOT enough: gnuplot skips those rows but still joins the points
# either side, drawing a smooth ramp across the void, inventing a steady climb
# through 2020-21 and a crash through 2022 that never happened. Substituting NaN
# is what actually breaks the line. Verified empirically; do not "simplify" this.
#
# A DOT OVERLAY SITS UNDER EACH TREND LINE. Breaking the line on NaN has one
# nasty consequence: a lone valid point with NaN on both sides draws NOTHING at
# all -- `with lines` needs a neighbour and `with filledcurves` needs a span.
# Measured on gnuplot 6.0: a 400,000-posting complete row between two partial
# rows stretched the y-axis to 400k and then drew an empty chart. Now that the
# nightly runs with --allow-partial, partial rows will be common and a complete
# crawl between two deadline days is exactly the value a reader most wants to
# see. The overlay is the same colour at pointsize 0.35, so it reads as part of
# the line rather than as separate marks. Measured against a render of this same
# script with the two overlay series removed: 966 of 1,872,000 pixels change by
# more than half a channel (0.05%), all of them on the existing line, and the
# isolated point that was previously undrawn gains 57. Do not enlarge it -- at
# 0.45 the line-thickening cost nearly doubles for 3 more pixels of signal.
#
# MOST ANNOTATIONS ARE POSITIONED IN GRAPH COORDINATES, not data values, so they
# stay put when the y-scale grows. The per-ATS rewrite multiplies postings
# several-fold, and hardcoded y positions would drift off or clip. The two
# annotations that point AT a specific measurement -- the peak label and the
# source-prune arrowhead -- are the exception and use `first` (data) coordinates:
# with the rewrite's ~473k rows in the series, `graph 0.97` resolves to ~485k and
# parked "peak 131,895" three and a half times higher than the peak it names.
#
# THE ANNOTATIONS ARE THE POINT. Unlabelled, the outages and methodology changes
# read as catastrophic market collapses; labelled, they read as what they are.
# Every date below was verified against the data and against git history.

# Verified era boundaries (jobs_record.txt) and their causes (git log).
outage1_start = "07/22/20"      # posting counts stop being written
outage1_end   = "06/25/21"      # resumes the day after ac49557 "Fix track_jobs.yml"
outage2_start = "01/27/22"      # counts stop again
outage2_end   = "11/07/22"      # resumes the day after 4dd3685 "Remove broken stuff"
peak_date     = "01/25/22"      # 131,895 postings, the all-time high
peak_value    = 131895          # labelled in data coords so it tracks the peak
prune_date    = "11/07/22"      # sources pruned: level shift, not a market move
prune_value   = 22332           # postings that day; where the arrowhead must land
rewrite_date  = "07/26/26"      # per-ATS rewrite: coverage widens sharply

# Rows added before crawl-quality tracking have no fourth column and are treated
# as complete. A partial row is a useful observation, but it must not join the
# completed trend line or fill its area. It is rendered as an isolated diamond.
postings = "(strcol(2) eq \"?\" || ($# >= 4 && strcol(4) eq \"partial\") ? NaN : real(strcol(2)))"
partial_postings = "($# >= 4 && strcol(4) eq \"partial\" ? real(strcol(2)) : NaN)"
sources = "($# >= 4 && strcol(4) eq \"partial\" ? NaN : real(strcol(3)))"
partial_sources = "($# >= 4 && strcol(4) eq \"partial\" ? real(strcol(3)) : NaN)"

if (!exists("datafile")) datafile = "jobs_record.txt"
if (!exists("outputfile")) outputfile = "jobs_record.png"

# Y-TICS ARE DERIVED FROM THE DATA, NOT HARDCODED. The per-ATS rewrite grew the
# posting ceiling 10x and the source ceiling 17x overnight; a fixed tic
# increment (formerly `set ytics 300` on the source panel) turned into thirty
# overlapping labels, and "%.0s%c" rendered 1.0M/1.1M/1.2M all as "1M". Instead:
# measure each series' max, pick a 1/2/2.5/5×10^n step that yields ~5 tics, and
# print each label with %g so 250k stays "250k" and 1.25M stays "1.25M".
#
# stats MUST run before `set xdata time`: with time mode on, stats refuses the
# datafile even when the analysed column is not the time column. Every *_max is
# read through exists() because stats DELETES its whole name-prefix and then
# defines nothing when a series is all-NaN (e.g. a record with no partial rows
# yet) — an unguarded reference would abort the render.
stats datafile using (@postings) nooutput name "P"
stats datafile using (@partial_postings) nooutput name "PP"
stats datafile using (@sources) nooutput name "S"
stats datafile using (@partial_sources) nooutput name "SP"
pmax = exists("P_max") ? P_max : 0
pmax = (exists("PP_max") && PP_max > pmax) ? PP_max : pmax
smax = exists("S_max") ? S_max : 0
smax = (exists("SP_max") && SP_max > smax) ? SP_max : smax
if (pmax <= 0) { pmax = 1 }
if (smax <= 0) { smax = 1 }

nicestep(m) = (raw = m / 6.0, p = 10.0 ** floor(log10(raw)), r = raw / p, \
    r <= 1 ? p : r <= 2 ? 2*p : r <= 2.5 ? 2.5*p : r <= 5 ? 5*p : 10*p)
fmtval(v) = v >= 1e6 ? sprintf("%gM", v/1e6) : \
            v >= 1e3 ? sprintf("%gk", v/1e3) : sprintf("%g", v)
ticlist(m) = (step = nicestep(m), out = "", n = int(m/step) + 1, \
    sum [i=0:n] (out = out . (i > 0 ? ", " : "") \
        . sprintf('"%s" %d', fmtval(i*step), i*step), 0), out)

# The headline stat is the latest complete measurement, printed top-right so
# the current number needs no squinting at the y-axis. awk, not gnuplot,
# because "last complete row" is a scan the stats command cannot express, and
# the digit grouping is hand-rolled because printf's %'d flag is a no-op in the
# C locale CI runs under. Empty (e.g. on a platform without awk) degrades to no
# stat line, never to an error.
latest = system("awk 'function c(n, s,o){s=sprintf(\"%d\",n); o=\"\"; while (length(s) > 3) { o=\",\" substr(s,length(s)-2) o; s=substr(s,1,length(s)-3) } return s o} $2 != \"?\" && $4 != \"partial\" { d=$1; p=$2; s=$3 } END { if (d != \"\") printf \"%s postings from %s sources (%s)\", c(p), c(s), d }' " . datafile)

set output outputfile
# noenhanced: without it, gnuplot reads "_" as a subscript marker and renders
# "jobs_record.txt" as "jobs" + subscript "r" + "ecord.txt".
set terminal pngcairo size 1800,1040 font ",15" background rgb '#fcfcfb' noenhanced

set xdata time
set timefmt "%m/%d/%y"

set style line 100 linecolor rgb '#e1e0d9' linewidth 1
set grid ytics back linestyle 100

set border 3 linecolor rgb '#c3c2b7' linewidth 1
set tics nomirror out scale 0.4
set tics textcolor rgb '#898781'

set style line 1 linecolor rgb '#2a78d6' linewidth 2
set style line 2 linecolor rgb '#eb6834' linewidth 2
set style line 3 linecolor rgb '#7a5195' pointtype 13 pointsize 1.7 linewidth 2
# Dot overlays: same hue as their trend line, small enough to disappear beneath
# it. Their only job is to render a point the line cannot reach (see DESIGN
# NOTES). Do not enlarge them -- at pointsize 0.5 the 2,046 daily points start
# to visibly thicken the six-year line.
set style line 11 linecolor rgb '#2a78d6' pointtype 7 pointsize 0.35
set style line 12 linecolor rgb '#eb6834' pointtype 7 pointsize 0.35
set style fill transparent solid 0.14 noborder

set key off

set lmargin at screen 0.075
set rmargin at screen 0.978

set multiplot

# ---- headings ----------------------------------------------------------------
set label 1 "Job postings tracked over time" at screen 0.075, 0.963 \
    font ",25" textcolor rgb '#0b0b0b' front
set label 2 "Daily totals from job boards crawled directly. Diamonds are deadline snapshots; shaded spans are missing counts." \
    at screen 0.075, 0.928 font ",14" textcolor rgb '#52514e' front
# The latest-crawl stat mirrors the title line, right-aligned, so it reads as
# the dashboard's headline number without competing with the subtitle.
if (latest ne "") \
    set label 3 latest at screen 0.978, 0.963 right font ",16" \
        textcolor rgb '#2a78d6' front

# ---- panel 1: postings -------------------------------------------------------
set tmargin at screen 0.880
set bmargin at screen 0.395

set ylabel "Postings" font ",14" textcolor rgb '#52514e' offset 1,0
set yrange [0:*]                # autoscale: the rewrite raises the ceiling sharply
eval("set ytics (" . ticlist(pmax) . ") font ',13'")

set format x ""
unset xlabel

# Outage bands go on THIS panel only. The source count kept being recorded during
# both outages, so shading the lower panel would contradict its own caption.
set object 1 rect from outage1_start, graph 0 to outage1_end, graph 1 \
    fillcolor rgb '#e1e0d9' fillstyle solid 0.5 noborder behind
set object 2 rect from outage2_start, graph 0 to outage2_end, graph 1 \
    fillcolor rgb '#e1e0d9' fillstyle solid 0.5 noborder behind

set label 10 "no counts recorded\nworkflow silently broken\n(fixed Jun 2021)" \
    at "01/05/21", graph 0.74 center font ",11" textcolor rgb '#52514e' front
set label 11 "no counts recorded\n(fixed Nov 2022)" \
    at "06/17/22", graph 0.74 center font ",11" textcolor rgb '#52514e' front

set label 12 "peak 131,895" at peak_date, first peak_value right offset -0.7,0.9 \
    font ",11" textcolor rgb '#2a78d6' front

# The arrowhead must land just above the 22,332 postings actually recorded on
# the prune date; a graph-coordinate head drifted 68k clear of the line as soon
# as the rewrite raised the ceiling. The tail stays in graph coordinates because
# it only has to sit beside its label.
set arrow 20 from "05/01/23", graph 0.35 to prune_date, first prune_value * 1.35 \
    linecolor rgb '#898781' linewidth 1 filled size 9,20
set label 13 "broken sources removed:\nlevel shift, not a market move" \
    at "05/20/23", graph 0.39 left font ",11" textcolor rgb '#52514e' front

set arrow 21 from rewrite_date, graph 0 to rewrite_date, graph 1 \
    nohead linecolor rgb '#898781' linewidth 1 dashtype (6,5) front
set label 14 "crawler rewritten per-ATS:\nfar wider coverage" \
    at rewrite_date, graph 0.87 right offset -1,0 font ",11" textcolor rgb '#898781' front

plot datafile using 1:@postings with filledcurves y1=0 \
         linecolor rgb '#2a78d6' notitle, \
     datafile using 1:@postings with lines linestyle 1 notitle, \
     datafile using 1:@postings with points linestyle 11 notitle, \
     datafile using 1:@partial_postings with points linestyle 3 notitle

# ---- panel 2: sources --------------------------------------------------------
unset label 10; unset label 11; unset label 12; unset label 13; unset label 14
unset arrow 20
unset object 1; unset object 2

set tmargin at screen 0.305
set bmargin at screen 0.115

set ylabel "Sources" font ",14" textcolor rgb '#52514e' offset 1,0
set yrange [0:*]
eval("set ytics (" . ticlist(smax) . ") font ',13'")

set format x "%Y"
set xtics 31557600 font ",13"

set label 15 "The source count kept being recorded throughout both outages; only the posting count was lost." \
    at screen 0.075, 0.062 font ",11" textcolor rgb '#898781' front
set label 16 "Generated by jobs_record.gnuplot from jobs_record.txt. See docs/jobs-record.md." \
    at screen 0.075, 0.030 font ",10" textcolor rgb '#898781' front

plot datafile using 1:@sources with filledcurves y1=0 linecolor rgb '#eb6834' notitle, \
     datafile using 1:@sources with lines linestyle 2 notitle, \
     datafile using 1:@sources with points linestyle 12 notitle, \
     datafile using 1:@partial_sources with points linestyle 3 notitle

unset multiplot
