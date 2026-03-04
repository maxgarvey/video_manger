
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

1. can you improve the trim video functionality to accept a draggable UI bar underneath the progress bar of the playthrough, representing the region to keep, and allow that to be finetuned vith finer-grained numerical inputs. On update, move the video to the exact spot the crop would take place
2. if you hit the crop button while the video is playing, stop playback and highlight the region from the current spot to the end of the video, effectively enabling easy deletion of the first part of a video
3. add clear labeling during trim mode that you're selecting the region to keep
4. make a confirmation after the user has highlighted a region or entered video times
5. organize season and episodes under specific shows
6. Add UI indicators on videos for if it's a tv show or a movie or a concert video or a vlog or a blog or a youtube
7. Support thumbnails in the UI for videos
8. Automatically generate a thumbnail image from the video. Create a button to randomly regenerate this
9. if the video has played to 70% or more, assume that we want to delete the end off the video by selecting the entire front region to keep when trim is clicked
10. Add a "Quck label" button to the info pane that will pop up a small modal to enter Title, Movie/TV, Season, Episode, Genre, etc.
