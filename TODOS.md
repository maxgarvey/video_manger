
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

1. Add a way to delete a video from the UI of viewing the video
2. Don't display "Loading..." on first load when there's no videos available
3. Allow directory creation via the library and moving videos into directories
4. check test coverage, check why CI build is failing in github, fix tests and CI to be green
