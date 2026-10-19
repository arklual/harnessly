
var input=document.querySelector('.filter');
if(input){input.addEventListener('input',function(){var q=input.value.trim().toLowerCase();document.querySelectorAll('li[data-title]').forEach(function(li){li.hidden=q!==''&&li.getAttribute('data-title').indexOf(q)<0;});});}
