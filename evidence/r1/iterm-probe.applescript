on run argv
	set dir to item 1 of argv
	tell application "iTerm2"
		set w to (create window with default profile)
		tell current session of w to write text "cd -- '" & dir & "' && echo \"reopen probe tab 1: $PWD\""
		tell w
			set t to (create tab with default profile)
			tell current session of t to write text "echo 'reopen probe tab 2' && printf '%s\\n' \"it's quoted\""
		end tell
		return id of w
	end tell
end run
