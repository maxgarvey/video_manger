
Please iteratively implement the following features, using best judgement.
Go one at a time, planning the change to behavior, then implementing the 
feature change, then implementing tests, then running tests, and then when
satisfied with the results, committing and pushing the changes. If expectations
aren't met, go back to planning with the new context in mind.

When you're planning, make files in a top level directory of the repository
called "plans" with a markdown file with the plan for the change in it.

I want you to proceed without intervention until you run out of credits and
then resume when you're able to again. Please update another document also
in the plans folder called "goals implementation status" with current status.

Before fine-grained planning, read the list and arrange them in the order that
makes the most sense. With a bias toward ease of complexity and implementation.

1. can you make it so that downloading via yt-dlp can support a queue of links
2. can you make it so that progress of that queue is visible in the UI
3. can you make it so that yt-dlp downloads metadata (be sure to clean up the metadata) and use the metadata to tag the video file at a file level on import? 
