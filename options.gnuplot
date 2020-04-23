set title "Total Job Postings Over Time"
set xlabel "Time"
set ylabel "Job Postings"
set xdata time
set timefmt "%m/%d/%y"
set terminal png size 800,500
set output "jobs_record.png"
plot "jobs_record.txt" using 1:2 notitle