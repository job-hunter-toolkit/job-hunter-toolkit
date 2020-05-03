set output "jobs_record.png"
set terminal png size 1000, 500
set timefmt "%m/%d/%y"
set title "Total Job Postings Over Time"
set xlabel "Time"
set xdata time
set format x "%m/%d/%y"
set ylabel "Job Postings"
set y2label "Job Sources"
set y2tics nomirror
set autoscale y
set autoscale y2
set autoscale x
set border linewidth 1.5
set style line 1 linecolor rgb '#0060ad' linetype 1 linewidth 2
set style line 2 linecolor rgb '#dd181f' linetype 1 linewidth 2
set key top right
plot "jobs_record.txt" using 1:2 title 'Job Postings' with lines axes x1y1 ls 1, \
     "jobs_record.txt" using 1:3 title 'Job Sources' with lines axes x1y2 ls 2