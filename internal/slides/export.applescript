on run argv
	set src to POSIX file (item 1 of argv)
	set dest to POSIX file (item 2 of argv)
	tell application id "com.apple.Keynote"
		set d to open src
		try
			export d to dest as PDF with properties {all stages:true, skipped slides:false, PDF image quality:Best}
		on error msg number n
			close d saving no
			error msg number n
		end try
		close d saving no
	end tell
end run
